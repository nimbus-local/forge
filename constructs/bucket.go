package constructs

import (
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// BucketArgs configures an S3 bucket construct.
type BucketArgs struct {
	// Public allows anonymous GET access (for static website assets).
	Public bool
	// Versioning enables object versioning.
	Versioning bool
	// CORS enables CORS for browser uploads.
	CORS bool
	// CORSAllowOrigins defaults to ["*"] when CORS is true.
	CORSAllowOrigins []string
	// KMSKeyArn is the ARN of a customer-managed KMS key for SSE-KMS encryption.
	// When set, all objects are encrypted with the specified key.
	KMSKeyArn pulumi.StringInput
	// LifecycleDays expires current objects after N days. 0 means no lifecycle rule.
	// When Versioning is also true, noncurrent versions are expired at the same age.
	LifecycleDays int
}

// Bucket is an S3 bucket construct.
type Bucket struct {
	name     string
	resource *s3.Bucket
	ctx      *forge.RunContext
}

// NewBucket creates an S3 bucket construct.
func NewBucket(ctx *forge.RunContext, name string, args *BucketArgs) *Bucket {
	if args == nil {
		args = &BucketArgs{}
	}

	pctx := ctx.Pulumi()

	bucketArgs := &s3.BucketArgs{
		Bucket: pulumi.String(bucketName(ctx, name)),
		Tags:   defaultTags(ctx, name),
	}

	if args.CORS {
		origins := args.CORSAllowOrigins
		if len(origins) == 0 {
			origins = []string{"*"}
		}
		bucketArgs.CorsRules = s3.BucketCorsRuleArray{
			&s3.BucketCorsRuleArgs{
				AllowedHeaders: pulumi.StringArray{pulumi.String("*")},
				AllowedMethods: pulumi.StringArray{
					pulumi.String("GET"),
					pulumi.String("PUT"),
					pulumi.String("POST"),
					pulumi.String("DELETE"),
					pulumi.String("HEAD"),
				},
				AllowedOrigins: toStringArray(origins),
				MaxAgeSeconds:  pulumi.Int(3000),
			},
		}
	}

	bucket, err := s3.NewBucket(pctx, name, bucketArgs)
	panicOnErr(err, name+": s3 bucket")

	if args.Versioning {
		_, err = s3.NewBucketVersioningV2(pctx, name+"-versioning", &s3.BucketVersioningV2Args{
			Bucket: bucket.Bucket,
			VersioningConfiguration: &s3.BucketVersioningV2VersioningConfigurationArgs{
				Status: pulumi.String("Enabled"),
			},
		})
		panicOnErr(err, name+": bucket versioning")
	}

	// Block all public access unless explicitly set to Public.
	if !args.Public {
		_, err = s3.NewBucketPublicAccessBlock(pctx, name+"-block", &s3.BucketPublicAccessBlockArgs{
			Bucket:                bucket.ID(),
			BlockPublicAcls:       pulumi.Bool(true),
			BlockPublicPolicy:     pulumi.Bool(true),
			IgnorePublicAcls:      pulumi.Bool(true),
			RestrictPublicBuckets: pulumi.Bool(true),
		})
		panicOnErr(err, name+": public access block")
	}

	// SSE-KMS encryption with a customer-managed key.
	if args.KMSKeyArn != nil {
		_, err = s3.NewBucketServerSideEncryptionConfigurationV2(pctx, name+"-sse", &s3.BucketServerSideEncryptionConfigurationV2Args{
			Bucket: bucket.ID(),
			Rules: s3.BucketServerSideEncryptionConfigurationV2RuleArray{
				&s3.BucketServerSideEncryptionConfigurationV2RuleArgs{
					ApplyServerSideEncryptionByDefault: &s3.BucketServerSideEncryptionConfigurationV2RuleApplyServerSideEncryptionByDefaultArgs{
						SseAlgorithm:   pulumi.String("aws:kms"),
						KmsMasterKeyId: args.KMSKeyArn,
					},
					BucketKeyEnabled: pulumi.Bool(true),
				},
			},
		})
		panicOnErr(err, name+": bucket sse config")
	}

	// Object lifecycle rule — expire current objects (and noncurrent versions if versioning
	// is enabled) after the specified number of days.
	if args.LifecycleDays > 0 {
		rule := &s3.BucketLifecycleConfigurationV2RuleArgs{
			Id:     pulumi.String("expire"),
			Status: pulumi.String("Enabled"),
			Expiration: &s3.BucketLifecycleConfigurationV2RuleExpirationArgs{
				Days: pulumi.Int(args.LifecycleDays),
			},
		}
		if args.Versioning {
			rule.NoncurrentVersionExpiration = &s3.BucketLifecycleConfigurationV2RuleNoncurrentVersionExpirationArgs{
				NoncurrentDays: pulumi.Int(args.LifecycleDays),
			}
		}
		_, err = s3.NewBucketLifecycleConfigurationV2(pctx, name+"-lifecycle", &s3.BucketLifecycleConfigurationV2Args{
			Bucket: bucket.ID(),
			Rules:  s3.BucketLifecycleConfigurationV2RuleArray{rule},
		})
		panicOnErr(err, name+": bucket lifecycle")
	}

	return &Bucket{name: name, resource: bucket, ctx: ctx}
}

// Name returns the physical bucket name as a Pulumi output.
func (b *Bucket) Name() pulumi.StringOutput { return b.resource.Bucket }

// ARN returns the bucket ARN as a Pulumi output.
func (b *Bucket) ARN() pulumi.StringOutput { return b.resource.Arn }

// LinkEnv implements Linkable — injects bucket name and ARN into linked Lambdas.
func (b *Bucket) LinkEnv() pulumi.StringMap {
	key := envKey(b.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_BUCKET_%s_NAME", key): b.resource.Bucket,
		fmt.Sprintf("SST_BUCKET_%s_ARN", key):  b.resource.Arn,
	}
}

// LinkName implements Linkable.
func (b *Bucket) LinkName() string { return b.name }
