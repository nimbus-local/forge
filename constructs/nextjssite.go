package constructs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudfront"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// NextjsSite deploys a Next.js application to AWS using open-next.
// Static assets are served from S3 via CloudFront; SSR and API routes run in
// a Lambda function behind a CloudFront behaviour.
//
// Prerequisites:
//  1. Install open-next: npm install --save-dev open-next
//  2. Set output: 'standalone' is NOT required — open-next handles bundling.
//
// Usage:
//
//	site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
//	    Path: ".",
//	    Link: []forge.Linkable{table, bucket},
//	})
//	ctx.Export("url", site.URL())
type NextjsSite struct {
	name         string
	distribution *cloudfront.Distribution
	role         *iam.Role
	ctx          *forge.RunContext
}

// NextjsSiteArgs configures a NextjsSite construct.
type NextjsSiteArgs struct {
	// Path is the root of the Next.js project directory. Defaults to ".".
	Path string
	// Environment variables injected into the open-next build AND the Lambda runtime.
	// Note: Pulumi output values are not available at build time; they ARE available
	// at Lambda runtime via the Link field.
	Environment map[string]string
	// Link injects linked resource env vars into the Lambda runtime environment.
	// The same SST_* naming convention used by AWS Lambda constructs applies.
	Link []forge.Linkable
	// MemorySize for the SSR Lambda in MB. Defaults to 1024.
	MemorySize int
	// Timeout for the SSR Lambda in seconds. Defaults to 30.
	Timeout int
	// Domain is an optional custom hostname (e.g. "www.example.com").
	// Requires DomainCertArn.
	Domain string
	// DomainCertArn is the ACM certificate ARN covering Domain.
	// Must be in us-east-1 (CloudFront requirement).
	DomainCertArn string
	// PriceClass limits the CloudFront edge network.
	// Defaults to "PriceClass_100" (US + Europe).
	PriceClass string
}

// NewNextjsSite creates a Next.js site construct backed by S3 + CloudFront + Lambda.
//
// On every deploy it runs: npx --yes open-next@latest build
// Then uploads .open-next/assets/ to S3 and deploys .open-next/server-function/
// as a Node.js 20.x Lambda.
func NewNextjsSite(ctx *forge.RunContext, name string, args *NextjsSiteArgs) *NextjsSite {
	if args == nil {
		args = &NextjsSiteArgs{}
	}
	projectPath := args.Path
	if projectPath == "" {
		projectPath = "."
	}
	if args.MemorySize == 0 {
		args.MemorySize = 1024
	}
	if args.Timeout == 0 {
		args.Timeout = 30
	}
	if args.PriceClass == "" {
		args.PriceClass = "PriceClass_100"
	}
	if args.Domain != "" && args.DomainCertArn == "" {
		panic("forge: NextjsSiteArgs.DomainCertArn is required when Domain is set for " + name)
	}

	absPath, err := filepath.Abs(projectPath)
	panicOnErr(err, name+": resolve project path")

	// ── Build with open-next ──────────────────────────────────────────────────
	runShellCmd("npm install", absPath, nil, name+": npm install")
	runShellCmd("npx --yes open-next@latest build", absPath, args.Environment, name+": open-next build")

	openNextDir := filepath.Join(absPath, ".open-next")
	assetsDir := filepath.Join(openNextDir, "assets")
	// open-next v3 uses server-functions/default; v2 used server-function.
	serverFnDir := filepath.Join(openNextDir, "server-functions", "default")
	if _, err := os.Stat(serverFnDir); err != nil {
		serverFnDir = filepath.Join(openNextDir, "server-function")
	}

	pctx := ctx.Pulumi()
	const s3OriginID = "s3-assets"
	const lambdaOriginID = "lambda-server"

	// ── Private S3 bucket for static assets ──────────────────────────────────
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

	oac, err := cloudfront.NewOriginAccessControl(pctx, name+"-oac", &cloudfront.OriginAccessControlArgs{
		Name:                          pulumi.String(qualifiedName(ctx, name)),
		OriginAccessControlOriginType: pulumi.String("s3"),
		SigningBehavior:               pulumi.String("always"),
		SigningProtocol:               pulumi.String("sigv4"),
	})
	panicOnErr(err, name+": origin access control")

	// ── IAM role for SSR Lambda ───────────────────────────────────────────────
	role, err := iam.NewRole(pctx, name+"-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version":"2012-10-17",
			"Statement":[{
				"Effect":"Allow",
				"Principal":{"Service":"lambda.amazonaws.com"},
				"Action":"sts:AssumeRole"
			}]
		}`),
		ManagedPolicyArns: pulumi.StringArray{
			pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
		},
		Tags: defaultTags(ctx, name),
	})
	panicOnErr(err, name+": iam role")

	_, err = cloudwatch.NewLogGroup(pctx, name+"-logs", &cloudwatch.LogGroupArgs{
		Name:            pulumi.Sprintf("/aws/lambda/%s", qualifiedName(ctx, name+"-server")),
		RetentionInDays: pulumi.Int(14),
		Tags:            defaultTags(ctx, name),
	})
	panicOnErr(err, name+": log group")

	// ── Lambda environment variables ──────────────────────────────────────────
	envVars := pulumi.StringMap{
		"FORGE_STAGE": pulumi.String(ctx.Stage),
		"NODE_ENV":    pulumi.String("production"),
	}
	for k, v := range args.Environment {
		envVars[k] = pulumi.String(v)
	}
	for _, link := range args.Link {
		for k, v := range link.LinkEnv() {
			envVars[k] = v
		}
	}

	// ── SSR Lambda function ───────────────────────────────────────────────────
	fn, err := awslambda.NewFunction(pctx, name+"-server", &awslambda.FunctionArgs{
		Name:          pulumi.String(qualifiedName(ctx, name+"-server")),
		Role:          role.Arn,
		Runtime:       pulumi.String("nodejs24.x"),
		Handler:       pulumi.String("index.handler"),
		Architectures: pulumi.StringArray{pulumi.String("arm64")},
		MemorySize:    pulumi.Int(args.MemorySize),
		Timeout:       pulumi.Int(args.Timeout),
		Code:          pulumi.NewFileArchive(serverFnDir),
		Environment: &awslambda.FunctionEnvironmentArgs{
			Variables: envVars,
		},
		Tags: defaultTags(ctx, name),
	})
	panicOnErr(err, name+": lambda function")

	// AuthorizationType NONE + two public resource-based policy statements is required for
	// CloudFront → Lambda Function URL to work. AWS_IAM requires SigV4 signing which
	// CloudFront does not perform for Lambda URL origins without a full OAC setup that
	// currently doesn't work reliably. The resource-based policy must grant BOTH
	// lambda:InvokeFunctionUrl and lambda:InvokeFunction to Principal "*".
	fnURL, err := awslambda.NewFunctionUrl(pctx, name+"-fn-url", &awslambda.FunctionUrlArgs{
		FunctionName:      fn.Name,
		AuthorizationType: pulumi.String("NONE"),
	})
	panicOnErr(err, name+": function url")

	// CloudFront needs just the hostname (no scheme/trailing slash).
	lambdaDomain := fnURL.FunctionUrl.ApplyT(func(u string) string {
		u = strings.TrimPrefix(u, "https://")
		u = strings.TrimPrefix(u, "http://")
		return strings.TrimSuffix(u, "/")
	}).(pulumi.StringOutput)

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
		Enabled:    pulumi.Bool(true),
		PriceClass: pulumi.String(args.PriceClass),
		Origins: cloudfront.DistributionOriginArray{
			// S3: static assets
			&cloudfront.DistributionOriginArgs{
				OriginId:              pulumi.String(s3OriginID),
				DomainName:            bucket.BucketRegionalDomainName,
				OriginAccessControlId: oac.ID(),
				S3OriginConfig: &cloudfront.DistributionOriginS3OriginConfigArgs{
					OriginAccessIdentity: pulumi.String(""),
				},
			},
			// Lambda Function URL: SSR + API routes
			&cloudfront.DistributionOriginArgs{
				OriginId:   pulumi.String(lambdaOriginID),
				DomainName: lambdaDomain,
				CustomOriginConfig: &cloudfront.DistributionOriginCustomOriginConfigArgs{
					HttpsPort:            pulumi.Int(443),
					HttpPort:             pulumi.Int(80),
					OriginProtocolPolicy: pulumi.String("https-only"),
					OriginSslProtocols:   pulumi.StringArray{pulumi.String("TLSv1.2")},
				},
			},
		},
		// Default: all requests → Lambda (SSR)
		DefaultCacheBehavior: &cloudfront.DistributionDefaultCacheBehaviorArgs{
			AllowedMethods:       pulumi.StringArray{pulumi.String("DELETE"), pulumi.String("GET"), pulumi.String("HEAD"), pulumi.String("OPTIONS"), pulumi.String("PATCH"), pulumi.String("POST"), pulumi.String("PUT")},
			CachedMethods:        pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
			TargetOriginId:       pulumi.String(lambdaOriginID),
			ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
			Compress:             pulumi.Bool(true),
			// AWS managed policy: CachingDisabled (pass-through to Lambda)
			CachePolicyId: pulumi.String("4135ea2d-6df8-44a3-9df3-4b5a84be39ad"),
			// AWS managed policy: AllViewerExceptHostHeader (forward headers to Lambda)
			OriginRequestPolicyId: pulumi.String("b689b0a8-53d0-40ab-baf2-68738e2966ac"),
		},
		// /_next/static/* → S3 (immutable, long cache)
		OrderedCacheBehaviors: cloudfront.DistributionOrderedCacheBehaviorArray{
			&cloudfront.DistributionOrderedCacheBehaviorArgs{
				PathPattern:          pulumi.String("/_next/static/*"),
				TargetOriginId:       pulumi.String(s3OriginID),
				AllowedMethods:       pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
				CachedMethods:        pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
				ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
				Compress:             pulumi.Bool(true),
				// AWS managed policy: CachingOptimized
				CachePolicyId: pulumi.String("658327ea-f89d-4fab-a63d-7e88639e58f6"),
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

	// Grant public invoke access required for NONE auth type (both actions required).
	_, err = awslambda.NewPermission(pctx, name+"-fn-url-public", &awslambda.PermissionArgs{
		Action:              pulumi.String("lambda:InvokeFunctionUrl"),
		Function:            fn.Name,
		Principal:           pulumi.String("*"),
		FunctionUrlAuthType: pulumi.String("NONE"),
	})
	panicOnErr(err, name+": lambda function url permission")

	_, err = awslambda.NewPermission(pctx, name+"-fn-invoke-public", &awslambda.PermissionArgs{
		Action:    pulumi.String("lambda:InvokeFunction"),
		Function:  fn.Name,
		Principal: pulumi.String("*"),
	})
	panicOnErr(err, name+": lambda invoke permission")

	// ── Bucket policy: allow CloudFront OAC ───────────────────────────────────
	_, err = s3.NewBucketPolicy(pctx, name+"-assets-policy", &s3.BucketPolicyArgs{
		Bucket: bucket.ID(),
		Policy: pulumi.All(bucket.Arn, dist.Arn).ApplyT(func(v []interface{}) string {
			return cfBucketPolicy(v[0].(string), v[1].(string))
		}).(pulumi.StringOutput),
	})
	panicOnErr(err, name+": bucket policy")

	// ── Upload static assets from .open-next/assets ───────────────────────────
	syncDirToS3(pctx, name, bucket, assetsDir)

	return &NextjsSite{name: name, distribution: dist, role: role, ctx: ctx}
}

// URL returns the CloudFront distribution URL (https://…) as a Pulumi output.
func (n *NextjsSite) URL() pulumi.StringOutput {
	return n.distribution.DomainName.ApplyT(func(d string) string {
		return "https://" + d
	}).(pulumi.StringOutput)
}

// Role returns the IAM execution role for the SSR Lambda, allowing callers to
// attach additional policies (e.g. DynamoDB read access).
func (n *NextjsSite) Role() *iam.Role { return n.role }

// LinkEnv implements Linkable — injects the site URL into linked Lambdas.
func (n *NextjsSite) LinkEnv() pulumi.StringMap {
	return pulumi.StringMap{
		fmt.Sprintf("SST_SITE_%s_URL", envKey(n.name)): n.URL(),
	}
}

// LinkName implements Linkable.
func (n *NextjsSite) LinkName() string { return n.name }
