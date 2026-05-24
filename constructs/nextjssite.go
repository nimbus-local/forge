package constructs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudfront"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/s3"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// NextjsSite deploys a Next.js application to AWS using open-next.
// Static assets are served from S3 via CloudFront; SSR and API routes run in
// a Lambda function behind a CloudFront behaviour.
//
// On every deploy forge runs:
//
//	npm install
//	npx --yes open-next@latest build
//
// Then uploads .open-next/assets/ to S3 and deploys the server and (if present)
// image-optimization Lambdas behind CloudFront.
//
// A lightweight CloudFront viewer-request function copies the Host header to
// x-forwarded-host before forwarding to the Lambda origin, so server-side code
// (including next-auth) can derive the correct public URL without NEXTAUTH_URL.
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
	// KMSKeyArn is the ARN of a customer-managed KMS key applied to the SSR Lambda
	// environment variables, its CloudWatch log group, and the static assets S3 bucket.
	// A kms:Grant is created automatically for the SSR Lambda execution role.
	// The key policy must also allow the CloudWatch Logs service principal.
	KMSKeyArn pulumi.StringInput
	// LogRetentionDays sets CloudWatch log retention for the SSR (and image) Lambda log groups.
	// 0 = default (14 days), -1 = never expire.
	// Valid non-zero values: 1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731,
	// 1096, 1827, 2192, 2557, 2922, 3288, 3653.
	LogRetentionDays int

	// testOpenNextDir bypasses npm install + open-next build and uses this
	// directory directly as the .open-next/ output. For testing only.
	testOpenNextDir string
}

// NewNextjsSite creates a Next.js site construct backed by S3 + CloudFront + Lambda.
//
// A CloudFront viewer-request function copies Host → x-forwarded-host on every
// request to a Lambda origin so next-auth and other server-side code can derive
// the correct public URL without a hardcoded NEXTAUTH_URL env var.
//
// If .open-next/image-optimization-function/ is present after the build, an
// image optimisation Lambda is deployed and wired to a /_next/image* CloudFront
// behaviour automatically. No extra configuration is needed; next/image components
// will work out of the box.
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

	absPath, err := filepath.Abs(resolvePath(ctx, projectPath))
	panicOnErr(err, name+": resolve project path")

	// ── Build with open-next ──────────────────────────────────────────────────
	openNextDir := args.testOpenNextDir
	if openNextDir == "" {
		runShellCmd("npm install", absPath, nil, name+": npm install")
		runShellCmd("npx --yes open-next@latest build", absPath, args.Environment, name+": open-next build")
		openNextDir = filepath.Join(absPath, ".open-next")
	}
	assetsDir := filepath.Join(openNextDir, "assets")
	// open-next v3 uses server-functions/default; v2 used server-function.
	serverFnDir := filepath.Join(openNextDir, "server-functions", "default")
	if _, err := os.Stat(serverFnDir); err != nil {
		serverFnDir = filepath.Join(openNextDir, "server-function")
	}
	imgFnDir := filepath.Join(openNextDir, "image-optimization-function")
	hasImgFn := false
	if _, err := os.Stat(imgFnDir); err == nil {
		hasImgFn = true
	}

	pctx := ctx.Pulumi()
	const s3OriginID = "s3-assets"
	const lambdaOriginID = "lambda-server"
	const imageOptOriginID = "lambda-image"

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

	if args.KMSKeyArn != nil {
		_, err = s3.NewBucketServerSideEncryptionConfigurationV2(pctx, name+"-assets-sse", &s3.BucketServerSideEncryptionConfigurationV2Args{
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
		panicOnErr(err, name+": assets bucket sse config")
	}

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

	ssrLogArgs := &cloudwatch.LogGroupArgs{
		Name: pulumi.Sprintf("/aws/lambda/%s", qualifiedName(ctx, name+"-server")),
		Tags: defaultTags(ctx, name),
	}
	if r := resolveLogRetention(args.LogRetentionDays); r != 0 {
		ssrLogArgs.RetentionInDays = pulumi.Int(r)
	}
	if args.KMSKeyArn != nil {
		ssrLogArgs.KmsKeyId = args.KMSKeyArn
	}
	_, err = cloudwatch.NewLogGroup(pctx, name+"-logs", ssrLogArgs)
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
	ssrFnArgs := &awslambda.FunctionArgs{
		Name:          pulumi.String(qualifiedName(ctx, name+"-server")),
		Role:          role.Arn,
		Runtime:       pulumi.String(RuntimeNodeJS24),
		Handler:       pulumi.String("index.handler"),
		Architectures: pulumi.StringArray{pulumi.String(ArchARM64)},
		MemorySize:    pulumi.Int(args.MemorySize),
		Timeout:       pulumi.Int(args.Timeout),
		Code:          pulumi.NewFileArchive(serverFnDir),
		Environment: &awslambda.FunctionEnvironmentArgs{
			Variables: envVars,
		},
		Tags: defaultTags(ctx, name),
	}
	if args.KMSKeyArn != nil {
		ssrFnArgs.KmsKeyArn = args.KMSKeyArn
	}
	fn, err := awslambda.NewFunction(pctx, name+"-server", ssrFnArgs)
	panicOnErr(err, name+": lambda function")

	if args.KMSKeyArn != nil {
		kmsGrant(pctx, name+"-server", args.KMSKeyArn, role.Arn)
	}

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

	// ── CloudFront viewer-request function: Host → x-forwarded-host ──────────
	// Copies the viewer Host header (the CloudFront domain or custom domain) to
	// x-forwarded-host before forwarding to the Lambda origin. This lets
	// server-side code — including next-auth — derive the correct public URL
	// from the request without a hardcoded NEXTAUTH_URL environment variable.
	hostFwdFn, err := cloudfront.NewFunction(pctx, name+"-host-fwd", &cloudfront.FunctionArgs{
		Name:    pulumi.String(qualifiedName(ctx, name+"-host-fwd")),
		Runtime: pulumi.String("cloudfront-js-2.0"),
		Publish: pulumi.Bool(true),
		Code: pulumi.String(`function handler(event) {
  var req = event.request;
  req.headers["x-forwarded-host"] = { value: req.headers["host"].value };
  return req;
}`),
	})
	panicOnErr(err, name+": host-forward function")

	// ── Image optimisation Lambda (if open-next provides one) ─────────────────
	// open-next builds .open-next/image-optimization-function/ alongside the SSR
	// bundle. When present, forge deploys it as a second Lambda and wires a
	// /_next/image* CloudFront behaviour to it so next/image components work.
	var imgLambdaDomain pulumi.StringOutput
	if hasImgFn {
		imgRole, err := iam.NewRole(pctx, name+"-img-role", &iam.RoleArgs{
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
		panicOnErr(err, name+": image iam role")

		imgLogArgs := &cloudwatch.LogGroupArgs{
			Name: pulumi.Sprintf("/aws/lambda/%s", qualifiedName(ctx, name+"-image")),
			Tags: defaultTags(ctx, name),
		}
		if r := resolveLogRetention(args.LogRetentionDays); r != 0 {
			imgLogArgs.RetentionInDays = pulumi.Int(r)
		}
		if args.KMSKeyArn != nil {
			imgLogArgs.KmsKeyId = args.KMSKeyArn
		}
		_, err = cloudwatch.NewLogGroup(pctx, name+"-image-logs", imgLogArgs)
		panicOnErr(err, name+": image log group")

		// Grant the image Lambda read access to the S3 assets bucket.
		// The imageLoader in open-next is "s3", so the Lambda fetches source images
		// directly from S3 before optimising and returning them.
		_, err = iam.NewRolePolicy(pctx, name+"-img-s3-policy", &iam.RolePolicyArgs{
			Role: imgRole.Name,
			Policy: bucket.Arn.ApplyT(func(arn string) string {
				return fmt.Sprintf(`{
					"Version":"2012-10-17",
					"Statement":[{
						"Effect":"Allow",
						"Action":"s3:GetObject",
						"Resource":"%s/*"
					}]
				}`, arn)
			}).(pulumi.StringOutput),
		})
		panicOnErr(err, name+": image s3 policy")

		imgFn, err := awslambda.NewFunction(pctx, name+"-image", &awslambda.FunctionArgs{
			Name:          pulumi.String(qualifiedName(ctx, name+"-image")),
			Role:          imgRole.Arn,
			Runtime:       pulumi.String(RuntimeNodeJS22),
			Handler:       pulumi.String("index.handler"),
			Architectures: pulumi.StringArray{pulumi.String(ArchARM64)},
			MemorySize:    pulumi.Int(1536),
			Timeout:       pulumi.Int(25),
			Code:          pulumi.NewFileArchive(imgFnDir),
			Environment: &awslambda.FunctionEnvironmentArgs{
				Variables: pulumi.StringMap{
					"BUCKET_NAME": bucket.Bucket,
					"NODE_ENV":    pulumi.String("production"),
				},
			},
			Tags: defaultTags(ctx, name),
		})
		panicOnErr(err, name+": image lambda")

		imgFnURL, err := awslambda.NewFunctionUrl(pctx, name+"-img-url", &awslambda.FunctionUrlArgs{
			FunctionName:      imgFn.Name,
			AuthorizationType: pulumi.String("NONE"),
		})
		panicOnErr(err, name+": image function url")

		imgLambdaDomain = imgFnURL.FunctionUrl.ApplyT(func(u string) string {
			u = strings.TrimPrefix(u, "https://")
			u = strings.TrimPrefix(u, "http://")
			return strings.TrimSuffix(u, "/")
		}).(pulumi.StringOutput)

		_, err = awslambda.NewPermission(pctx, name+"-img-url-public", &awslambda.PermissionArgs{
			Action:              pulumi.String("lambda:InvokeFunctionUrl"),
			Function:            imgFn.Name,
			Principal:           pulumi.String("*"),
			FunctionUrlAuthType: pulumi.String("NONE"),
		})
		panicOnErr(err, name+": image lambda url permission")

		_, err = awslambda.NewPermission(pctx, name+"-img-invoke-public", &awslambda.PermissionArgs{
			Action:    pulumi.String("lambda:InvokeFunction"),
			Function:  imgFn.Name,
			Principal: pulumi.String("*"),
		})
		panicOnErr(err, name+": image lambda invoke permission")
	}

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

	origins := cloudfront.DistributionOriginArray{
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
	}
	if hasImgFn {
		origins = append(origins, &cloudfront.DistributionOriginArgs{
			OriginId:   pulumi.String(imageOptOriginID),
			DomainName: imgLambdaDomain,
			CustomOriginConfig: &cloudfront.DistributionOriginCustomOriginConfigArgs{
				HttpsPort:            pulumi.Int(443),
				HttpPort:             pulumi.Int(80),
				OriginProtocolPolicy: pulumi.String("https-only"),
				OriginSslProtocols:   pulumi.StringArray{pulumi.String("TLSv1.2")},
			},
		})
	}

	// Ordered behaviors: image optimisation first (if present), then static assets.
	orderedBehaviors := cloudfront.DistributionOrderedCacheBehaviorArray{}
	if hasImgFn {
		orderedBehaviors = append(orderedBehaviors, &cloudfront.DistributionOrderedCacheBehaviorArgs{
			PathPattern:          pulumi.String("/_next/image*"),
			TargetOriginId:       pulumi.String(imageOptOriginID),
			AllowedMethods:       pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
			CachedMethods:        pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
			ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
			Compress:             pulumi.Bool(true),
			// UseOriginCacheControlHeaders-Defaults: respects Cache-Control set by the image Lambda
			CachePolicyId: pulumi.String("83da9c7e-98b4-4e11-a168-04f0df8e2c65"),
			// AllViewerExceptHostHeader: forwards query strings (url, w, q) to the Lambda
			OriginRequestPolicyId: pulumi.String("b689b0a8-53d0-40ab-baf2-68738e2966ac"),
			FunctionAssociations: cloudfront.DistributionOrderedCacheBehaviorFunctionAssociationArray{
				&cloudfront.DistributionOrderedCacheBehaviorFunctionAssociationArgs{
					EventType:   pulumi.String("viewer-request"),
					FunctionArn: hostFwdFn.Arn,
				},
			},
		})
	}
	// /_next/static/* → S3 (immutable, long cache)
	orderedBehaviors = append(orderedBehaviors, &cloudfront.DistributionOrderedCacheBehaviorArgs{
		PathPattern:          pulumi.String("/_next/static/*"),
		TargetOriginId:       pulumi.String(s3OriginID),
		AllowedMethods:       pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
		CachedMethods:        pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
		ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
		Compress:             pulumi.Bool(true),
		// CachingOptimized: long-lived cache for content-hashed static assets
		CachePolicyId: pulumi.String("658327ea-f89d-4fab-a63d-7e88639e58f6"),
	})

	distArgs := &cloudfront.DistributionArgs{
		Enabled:    pulumi.Bool(true),
		PriceClass: pulumi.String(args.PriceClass),
		Origins:    origins,
		// Default: all requests → SSR Lambda with host-forward function
		DefaultCacheBehavior: &cloudfront.DistributionDefaultCacheBehaviorArgs{
			AllowedMethods:       pulumi.StringArray{pulumi.String("DELETE"), pulumi.String("GET"), pulumi.String("HEAD"), pulumi.String("OPTIONS"), pulumi.String("PATCH"), pulumi.String("POST"), pulumi.String("PUT")},
			CachedMethods:        pulumi.StringArray{pulumi.String("GET"), pulumi.String("HEAD")},
			TargetOriginId:       pulumi.String(lambdaOriginID),
			ViewerProtocolPolicy: pulumi.String("redirect-to-https"),
			Compress:             pulumi.Bool(true),
			// CachingDisabled: pass-through to Lambda
			CachePolicyId: pulumi.String("4135ea2d-6df8-44a3-9df3-4b5a84be39ad"),
			// AllViewerExceptHostHeader: forward headers to Lambda
			OriginRequestPolicyId: pulumi.String("b689b0a8-53d0-40ab-baf2-68738e2966ac"),
			FunctionAssociations: cloudfront.DistributionDefaultCacheBehaviorFunctionAssociationArray{
				&cloudfront.DistributionDefaultCacheBehaviorFunctionAssociationArgs{
					EventType:   pulumi.String("viewer-request"),
					FunctionArn: hostFwdFn.Arn,
				},
			},
		},
		OrderedCacheBehaviors: orderedBehaviors,
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
