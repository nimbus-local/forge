// Package bootstrap creates and validates the S3 bucket used for Pulumi state storage.
package bootstrap

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Config holds the parameters needed to locate or create the state bucket.
type Config struct {
	AppName    string
	Stage      string
	AWSProfile string
	AWSRegion  string
}

// BucketName returns the conventional state bucket name for the given app and stage.
func BucketName(appName, stage string) string {
	return fmt.Sprintf("%s-%s-forge-state", appName, stage)
}

// EnsureStateBucket creates the Pulumi state S3 bucket if it does not already exist.
// Returns true if the bucket was created, false if it already existed.
// Safe to call multiple times — idempotent.
func EnsureStateBucket(ctx context.Context, cfg Config) (created bool, err error) {
	client, region, err := newClient(ctx, cfg)
	if err != nil {
		return false, err
	}

	name := BucketName(cfg.AppName, cfg.Stage)

	exists, err := bucketExists(ctx, client, name)
	if err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}

	if err := createBucket(ctx, client, name, region); err != nil {
		return false, err
	}
	if err := applyBucketSettings(ctx, client, name); err != nil {
		return false, fmt.Errorf("configure bucket %s: %w", name, err)
	}

	return true, nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

func newClient(ctx context.Context, cfg Config) (*s3.Client, string, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.AWSProfile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(cfg.AWSProfile))
	}
	if cfg.AWSRegion != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.AWSRegion))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, "", fmt.Errorf("load aws config: %w", err)
	}
	region := awsCfg.Region
	if region == "" {
		region = "us-east-1"
	}
	return s3.NewFromConfig(awsCfg), region, nil
}

func bucketExists(ctx context.Context, client *s3.Client, name string) (bool, error) {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)})
	if err == nil {
		return true, nil
	}
	// 404 → bucket does not exist; anything else is a real error.
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return false, nil
	}
	// SDK v2 may surface 404s as a generic HTTP response error.
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) && httpErr.HTTPStatusCode() == 404 {
		return false, nil
	}
	return false, fmt.Errorf("head bucket %s: %w", name, err)
}

func createBucket(ctx context.Context, client *s3.Client, name, region string) error {
	input := &s3.CreateBucketInput{Bucket: aws.String(name)}
	// us-east-1 does not accept a LocationConstraint.
	if region != "us-east-1" {
		input.CreateBucketConfiguration = &types.CreateBucketConfiguration{
			LocationConstraint: types.BucketLocationConstraint(region),
		}
	}
	if _, err := client.CreateBucket(ctx, input); err != nil {
		return fmt.Errorf("create bucket %s: %w", name, err)
	}
	return nil
}

// applyBucketSettings configures the state bucket with the settings required
// for safe, cost-efficient Pulumi state storage.
func applyBucketSettings(ctx context.Context, client *s3.Client, name string) error {
	// Block all public access.
	if _, err := client.PutPublicAccessBlock(ctx, &s3.PutPublicAccessBlockInput{
		Bucket: aws.String(name),
		PublicAccessBlockConfiguration: &types.PublicAccessBlockConfiguration{
			BlockPublicAcls:       aws.Bool(true),
			BlockPublicPolicy:     aws.Bool(true),
			IgnorePublicAcls:      aws.Bool(true),
			RestrictPublicBuckets: aws.Bool(true),
		},
	}); err != nil {
		return fmt.Errorf("block public access: %w", err)
	}

	// Enable versioning so state history is preserved.
	if _, err := client.PutBucketVersioning(ctx, &s3.PutBucketVersioningInput{
		Bucket: aws.String(name),
		VersioningConfiguration: &types.VersioningConfiguration{
			Status: types.BucketVersioningStatusEnabled,
		},
	}); err != nil {
		return fmt.Errorf("enable versioning: %w", err)
	}

	// Server-side encryption with S3-managed keys (SSE-S3).
	if _, err := client.PutBucketEncryption(ctx, &s3.PutBucketEncryptionInput{
		Bucket: aws.String(name),
		ServerSideEncryptionConfiguration: &types.ServerSideEncryptionConfiguration{
			Rules: []types.ServerSideEncryptionRule{{
				ApplyServerSideEncryptionByDefault: &types.ServerSideEncryptionByDefault{
					SSEAlgorithm: types.ServerSideEncryptionAes256,
				},
				BucketKeyEnabled: aws.Bool(true),
			}},
		},
	}); err != nil {
		return fmt.Errorf("enable encryption: %w", err)
	}

	// Expire non-current object versions after 90 days to control storage costs.
	days := int32(90)
	if _, err := client.PutBucketLifecycleConfiguration(ctx, &s3.PutBucketLifecycleConfigurationInput{
		Bucket: aws.String(name),
		LifecycleConfiguration: &types.BucketLifecycleConfiguration{
			Rules: []types.LifecycleRule{{
				ID:     aws.String("expire-old-state"),
				Status: types.ExpirationStatusEnabled,
				Filter: &types.LifecycleRuleFilter{Prefix: aws.String("")},
				NoncurrentVersionExpiration: &types.NoncurrentVersionExpiration{
					NoncurrentDays: &days,
				},
			}},
		},
	}); err != nil {
		return fmt.Errorf("set lifecycle policy: %w", err)
	}

	return nil
}
