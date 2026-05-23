package cloudflare

import (
	"fmt"

	cf "github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/nimbus-local/forge"
)

// R2BucketArgs configures a Cloudflare R2 bucket construct.
type R2BucketArgs struct {
	// Location sets the default storage class location hint (e.g. "enam", "weur", "apac").
	// Omit to let Cloudflare choose automatically.
	Location string
}

// R2Bucket is a Cloudflare R2 object-storage bucket construct.
type R2Bucket struct {
	name     string
	resource *cf.R2Bucket
	ctx      *forge.RunContext
}

// NewR2Bucket creates a Cloudflare R2 bucket.
func NewR2Bucket(ctx *forge.RunContext, name string, args *R2BucketArgs) *R2Bucket {
	if args == nil {
		args = &R2BucketArgs{}
	}

	pctx := ctx.Pulumi()

	bucketArgs := &cf.R2BucketArgs{
		AccountId: pulumi.String(accountID(ctx)),
		Name:      pulumi.String(qualifiedName(ctx, name)),
	}
	if args.Location != "" {
		bucketArgs.Location = pulumi.StringPtr(args.Location)
	}

	bucket, err := cf.NewR2Bucket(pctx, name, bucketArgs)
	panicOnErr(err, name+": r2 bucket")

	return &R2Bucket{name: name, resource: bucket, ctx: ctx}
}

// Name returns the physical bucket name as a Pulumi output.
func (r *R2Bucket) Name() pulumi.StringOutput { return r.resource.Name }

// LinkEnv implements forge.Linkable — injects the bucket name into linked Lambdas.
func (r *R2Bucket) LinkEnv() pulumi.StringMap {
	key := envKey(r.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_R2_%s_NAME", key): r.resource.Name,
	}
}

// LinkName implements forge.Linkable.
func (r *R2Bucket) LinkName() string { return r.name }
