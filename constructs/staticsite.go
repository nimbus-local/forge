package constructs

import (
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudfront"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// StaticSite serves a pre-built static website from S3 via CloudFront.
// Set OutputDir to the build output directory (e.g. "dist", "build", "out").
// Use the optional Build field to run a build step before uploading.
//
// Usage:
//
//	site := constructs.NewStaticSite(ctx, "Web", &constructs.StaticSiteArgs{
//	    Build:       "npm run build",
//	    OutputDir:   "dist",
//	    Environment: map[string]string{"VITE_API_URL": "https://api.example.com"},
//	})
//	ctx.Export("url", site.URL())
type StaticSite struct {
	name         string
	distribution *cloudfront.Distribution
	ctx          *forge.RunContext
}

// StaticSiteArgs configures a StaticSite construct.
type StaticSiteArgs struct {
	// OutputDir is the path to the pre-built asset directory (e.g. "dist", "out"). Required.
	OutputDir string
	// Build is an optional shell command to run before uploading (e.g. "npm run build").
	// Runs on every deploy. Executed via sh -c in BuildDir.
	Build string
	// BuildDir is the working directory for the Build command. Defaults to ".".
	BuildDir string
	// Environment variables injected into the Build process.
	// Note: Pulumi output values (e.g. API URLs from other constructs) are not
	// available here because the build runs before Pulumi creates resources.
	Environment map[string]string
	// IndexDocument is the CloudFront default root object. Defaults to "index.html".
	IndexDocument string
	// ErrorDocument is served for 403/404 responses. Defaults to "index.html"
	// (SPA-style catch-all routing).
	ErrorDocument string
	// Domain is an optional custom hostname (e.g. "www.example.com").
	// Requires DomainCertArn.
	Domain string
	// DomainCertArn is the ACM certificate ARN covering Domain.
	// Must be in us-east-1 (CloudFront requirement).
	DomainCertArn string
	// PriceClass limits the CloudFront edge network.
	// Defaults to "PriceClass_100" (US + Europe).
	// Other values: "PriceClass_200", "PriceClass_All".
	PriceClass string
}

// NewStaticSite creates an S3 + CloudFront static website construct.
// If Build is set, it runs the command before uploading assets.
func NewStaticSite(ctx *forge.RunContext, name string, args *StaticSiteArgs) *StaticSite {
	if args == nil {
		args = &StaticSiteArgs{}
	}
	if args.OutputDir == "" {
		panic("forge: StaticSiteArgs.OutputDir must be set for " + name)
	}
	if args.IndexDocument == "" {
		args.IndexDocument = "index.html"
	}
	if args.ErrorDocument == "" {
		args.ErrorDocument = "index.html"
	}
	if args.PriceClass == "" {
		args.PriceClass = "PriceClass_100"
	}
	if args.Domain != "" && args.DomainCertArn == "" {
		panic("forge: StaticSiteArgs.DomainCertArn is required when Domain is set for " + name)
	}

	// ── Optional build step ───────────────────────────────────────────────────
	if args.Build != "" {
		buildDir := args.BuildDir
		if buildDir == "" {
			buildDir = "."
		}
		runShellCmd(args.Build, buildDir, args.Environment, name+": build")
	}

	outputDir := resolvePath(ctx, args.OutputDir)

	pctx := ctx.Pulumi()
	const originID = "s3-assets"

	// ── Private S3 bucket ─────────────────────────────────────────────────────
	bucket, err := s3.NewBucket(pctx, name+"-assets", &s3.BucketArgs{
		Bucket:       pulumi.String(bucketName(ctx, name+"-assets")),
		ForceDestroy: pulumi.Bool(true),
		Tags:         defaultTags(ctx, name),
	})
	panicOnErr(err, name+": assets bucket")

	_, err = s3.NewBucketPublicAccessBlock(pctx, name+"-assets-block", &s3.BucketPublicAccessBlockArgs{
		Bucket:                bucket.ID(),
		BlockPublicAcls:       pulumi.Bool(true),
		BlockPublicPolicy:     pulumi.Bool(true),
		IgnorePublicAcls:      pulumi.Bool(true),
		RestrictPublicBuckets: pulumi.Bool(true),
	})
	panicOnErr(err, name+": public access block")

	// ── CloudFront Origin Access Control ──────────────────────────────────────
	oac, err := cloudfront.NewOriginAccessControl(pctx, name+"-oac", &cloudfront.OriginAccessControlArgs{
		Name:                          pulumi.String(qualifiedName(ctx, name)),
		OriginAccessControlOriginType: pulumi.String("s3"),
		SigningBehavior:               pulumi.String("always"),
		SigningProtocol:               pulumi.String("sigv4"),
	})
	panicOnErr(err, name+": origin access control")

	// ── CloudFront distribution ───────────────────────────────────────────────
	viewerCert := cloudfront.DistributionViewerCertificateArgs{
		CloudfrontDefaultCertificate: pulumi.Bool(true),
	}
	var aliases pulumi.StringArray
	if args.Domain != "" {
		aliases = pulumi.StringArray{pulumi.String(args.Domain)}
		viewerCert = cloudfront.DistributionViewerCertificateArgs{
			AcmCertificateArn:      pulumi.String(args.DomainCertArn),
			SslSupportMethod:       pulumi.String("sni-only"),
			MinimumProtocolVersion: pulumi.String("TLSv1.2_2021"),
		}
	}

	distArgs := &cloudfront.DistributionArgs{
		Enabled:           pulumi.Bool(true),
		DefaultRootObject: pulumi.String(args.IndexDocument),
		PriceClass:        pulumi.String(args.PriceClass),
		Origins: cloudfront.DistributionOriginArray{
			&cloudfront.DistributionOriginArgs{
				OriginId:              pulumi.String(originID),
				DomainName:            bucket.BucketRegionalDomainName,
				OriginAccessControlId: oac.ID(),
				S3OriginConfig: &cloudfront.DistributionOriginS3OriginConfigArgs{
					OriginAccessIdentity: pulumi.String(""),
				},
			},
		},
		DefaultCacheBehavior: &cloudfront.DistributionDefaultCacheBehaviorArgs{
			AllowedMethods:       pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD"), pulumi.String("OPTIONS")},
			CachedMethods:        pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
			TargetOriginId:       pulumi.String(originID),
			ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
			Compress:             pulumi.Bool(true),
			// AWS managed policy: CachingOptimized
			CachePolicyId: pulumi.String("658327ea-f89d-4fab-a63d-7e88639e58f6"),
		},
		// 403/404 → index.html for SPA routing
		CustomErrorResponses: cloudfront.DistributionCustomErrorResponseArray{
			&cloudfront.DistributionCustomErrorResponseArgs{
				ErrorCode:        pulumi.Int(403),
				ResponseCode:     pulumi.Int(200),
				ResponsePagePath: pulumi.Sprintf("/%s", args.ErrorDocument),
			},
			&cloudfront.DistributionCustomErrorResponseArgs{
				ErrorCode:        pulumi.Int(404),
				ResponseCode:     pulumi.Int(200),
				ResponsePagePath: pulumi.Sprintf("/%s", args.ErrorDocument),
			},
		},
		Restrictions: &cloudfront.DistributionRestrictionsArgs{
			GeoRestriction: &cloudfront.DistributionRestrictionsGeoRestrictionArgs{
				RestrictionType: pulumi.String("none"),
			},
		},
		ViewerCertificate: &viewerCert,
		Tags:              defaultTags(ctx, name),
	}
	if aliases != nil {
		distArgs.Aliases = aliases
	}

	dist, err := cloudfront.NewDistribution(pctx, name+"-cdn", distArgs)
	panicOnErr(err, name+": cloudfront distribution")

	// ── Bucket policy: allow CloudFront OAC ───────────────────────────────────
	_, err = s3.NewBucketPolicy(pctx, name+"-assets-policy", &s3.BucketPolicyArgs{
		Bucket: bucket.ID(),
		Policy: pulumi.All(bucket.Arn, dist.Arn).ApplyT(func(v []interface{}) string {
			return cfBucketPolicy(v[0].(string), v[1].(string))
		}).(pulumi.StringOutput),
	})
	panicOnErr(err, name+": bucket policy")

	// ── Upload assets ─────────────────────────────────────────────────────────
	syncDirToS3(pctx, name, bucket, outputDir)

	return &StaticSite{name: name, distribution: dist, ctx: ctx}
}

// URL returns the CloudFront distribution URL (https://…) as a Pulumi output.
func (s *StaticSite) URL() pulumi.StringOutput {
	return s.distribution.DomainName.ApplyT(func(d string) string {
		return "https://" + d
	}).(pulumi.StringOutput)
}

// LinkEnv implements Linkable — injects the site URL into linked Lambdas.
func (s *StaticSite) LinkEnv() pulumi.StringMap {
	return pulumi.StringMap{
		fmt.Sprintf("SST_SITE_%s_URL", envKey(s.name)): s.URL(),
	}
}

// LinkName implements Linkable.
func (s *StaticSite) LinkName() string { return s.name }

// ── shared site helpers ───────────────────────────────────────────────────────

// syncDirToS3 walks dir and creates one BucketObject per file.
// Pulumi tracks content hashes so only changed files are re-uploaded.
func syncDirToS3(pctx *pulumi.Context, name string, bucket *s3.Bucket, dir string) {
	err := filepath.Walk(dir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info.IsDir() {
			return walkErr
		}
		rel, _ := filepath.Rel(dir, path)
		key := filepath.ToSlash(rel)
		_, err := s3.NewBucketObject(pctx, siteResName(name, key), &s3.BucketObjectArgs{
			Bucket:       bucket.ID(),
			Key:          pulumi.String(key),
			Source:       pulumi.NewFileAsset(path),
			ContentType:  pulumi.String(detectMIME(path)),
			CacheControl: pulumi.String(siteCacheControl(key)),
		})
		return err
	})
	panicOnErr(err, name+": sync assets to S3")
}

// siteResName derives a stable Pulumi resource name from a file key.
// Replaces characters that would make the name ambiguous or invalid.
func siteResName(prefix, key string) string {
	safe := strings.NewReplacer("/", "--", ".", "-", " ", "-").Replace(key)
	return prefix + "-obj-" + safe
}

// detectMIME returns the MIME content type for the given file path.
func detectMIME(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	known := map[string]string{
		".html":        "text/html; charset=utf-8",
		".css":         "text/css; charset=utf-8",
		".js":          "application/javascript; charset=utf-8",
		".mjs":         "application/javascript; charset=utf-8",
		".json":        "application/json",
		".webmanifest": "application/manifest+json",
		".map":         "application/json",
		".svg":         "image/svg+xml",
		".png":         "image/png",
		".jpg":         "image/jpeg",
		".jpeg":        "image/jpeg",
		".gif":         "image/gif",
		".webp":        "image/webp",
		".ico":         "image/x-icon",
		".woff":        "font/woff",
		".woff2":       "font/woff2",
		".ttf":         "font/ttf",
		".txt":         "text/plain; charset=utf-8",
		".xml":         "text/xml; charset=utf-8",
	}
	if ct, ok := known[ext]; ok {
		return ct
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}

// siteCacheControl returns the Cache-Control header for an S3 object key.
// Versioned/hashed paths are marked immutable; HTML and other documents must
// always revalidate so stale content is never served after a deploy.
func siteCacheControl(key string) string {
	for _, prefix := range []string{"_next/static/", "assets/", "static/"} {
		if strings.HasPrefix(key, prefix) {
			return "public, max-age=31536000, immutable"
		}
	}
	return "public, max-age=0, must-revalidate"
}

// cfBucketPolicy returns a bucket policy JSON that allows a specific CloudFront
// distribution to call s3:GetObject via OAC.
func cfBucketPolicy(bucketARN, distARN string) string {
	return fmt.Sprintf(`{
		"Version":"2012-10-17",
		"Statement":[{
			"Effect":"Allow",
			"Principal":{"Service":"cloudfront.amazonaws.com"},
			"Action":"s3:GetObject",
			"Resource":"%s/*",
			"Condition":{"StringEquals":{"AWS:SourceArn":"%s"}}
		}]
	}`, bucketARN, distARN)
}

// runShellCmd executes a shell command synchronously.
// Stdout/stderr stream to the terminal. Panics on non-zero exit.
func runShellCmd(command, dir string, env map[string]string, context string) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	if err := cmd.Run(); err != nil {
		panic(fmt.Sprintf("forge [%s]: %v", context, err))
	}
}
