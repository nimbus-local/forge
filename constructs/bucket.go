package constructs

import (
	"fmt"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
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

	if args.Versioning {
		bucketArgs.Versioning = &s3.BucketVersioningArgs{
			Enabled: pulumi.Bool(true),
		}
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
