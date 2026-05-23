// Package bootstrap creates and validates the S3 bucket used for Pulumi state storage.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// Config holds the parameters needed to locate or create the state bucket.
type Config struct {
	AppName    string
	Stage      string
	AWSProfile string
	AWSRegion  string
}

// BucketName returns the conventional state bucket name for the given app, stage,
// and AWS account ID. The account ID suffix ensures global uniqueness across accounts.
func BucketName(appName, stage, accountID string) string {
	return fmt.Sprintf("%s-%s-forge-state-%s", appName, stage, accountID)
}

// s3API is the subset of *s3.Client used by bootstrap, extracted for testing.
type s3API interface {
	HeadBucket(ctx context.Context, params *s3.HeadBucketInput, optFns ...func(*s3.Options)) (*s3.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, params *s3.CreateBucketInput, optFns ...func(*s3.Options)) (*s3.CreateBucketOutput, error)
	PutPublicAccessBlock(ctx context.Context, params *s3.PutPublicAccessBlockInput, optFns ...func(*s3.Options)) (*s3.PutPublicAccessBlockOutput, error)
	PutBucketVersioning(ctx context.Context, params *s3.PutBucketVersioningInput, optFns ...func(*s3.Options)) (*s3.PutBucketVersioningOutput, error)
	PutBucketEncryption(ctx context.Context, params *s3.PutBucketEncryptionInput, optFns ...func(*s3.Options)) (*s3.PutBucketEncryptionOutput, error)
	PutBucketLifecycleConfiguration(ctx context.Context, params *s3.PutBucketLifecycleConfigurationInput, optFns ...func(*s3.Options)) (*s3.PutBucketLifecycleConfigurationOutput, error)
}

// EnsureStateBucket creates the Pulumi state S3 bucket if it does not already exist.
// Returns the bucket name, whether it was created, and any error.
// Safe to call multiple times — idempotent.
func EnsureStateBucket(ctx context.Context, cfg Config) (bucketName string, created bool, err error) {
	client, region, awsCfg, err := newClient(ctx, cfg)
	if err != nil {
		return "", false, err
	}
	accountID, err := resolveAccountID(ctx, awsCfg)
	if err != nil {
		return "", false, fmt.Errorf("resolve account ID: %w", err)
	}
	name := BucketName(cfg.AppName, cfg.Stage, accountID)
	created, err = ensureWithClient(ctx, client, name, region)
	return name, created, err
}

func resolveAccountID(ctx context.Context, awsCfg aws.Config) (string, error) {
	out, err := sts.NewFromConfig(awsCfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", err
	}
	return aws.ToString(out.Account), nil
}

// ensureWithClient is the testable core of EnsureStateBucket.
func ensureWithClient(ctx context.Context, client s3API, name, region string) (bool, error) {
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

func newClient(ctx context.Context, cfg Config) (*s3.Client, string, aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.AWSProfile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(cfg.AWSProfile))
	}
	if cfg.AWSRegion != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.AWSRegion))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, "", aws.Config{}, fmt.Errorf("load aws config: %w", err)
	}
	region := awsCfg.Region
	if region == "" {
		region = "us-east-1"
	}
	var clientOpts []func(*s3.Options)
	if endpoint := os.Getenv("FORGE_AWS_ENDPOINT"); endpoint != "" {
		clientOpts = append(clientOpts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // required for path-style URLs on local endpoints
		})
	}
	return s3.NewFromConfig(awsCfg, clientOpts...), region, awsCfg, nil
}

func bucketExists(ctx context.Context, client s3API, name string) (bool, error) {
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(name)})
	if err == nil {
		return true, nil
	}
	// 404 → bucket does not exist; anything else is a real error.
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return false, nil
	}
	// SDK v2 may surface 404s or 301s as generic HTTP response errors.
	var httpErr interface{ HTTPStatusCode() int }
	if errors.As(err, &httpErr) {
		switch httpErr.HTTPStatusCode() {
		case 404:
			return false, nil
		case 301:
			// Bucket exists but was created in a different region — treat as exists.
			return true, nil
		}
	}
	return false, fmt.Errorf("head bucket %s: %w", name, err)
}

func createBucket(ctx context.Context, client s3API, name, region string) error {
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
func applyBucketSettings(ctx context.Context, client s3API, name string) error {
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
