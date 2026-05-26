package constructs

import (
	"fmt"
	"os"
	"strings"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/sqs"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// Function creates an AWS Lambda function with sane defaults:
//   - Execution role with CloudWatch Logs permissions
//   - Log group with 14-day retention
//   - Environment variables injected from all linked resources
//   - X-Ray tracing enabled in non-dev stages
type Function struct {
	name     string
	resource *awslambda.Function
	role     *iam.Role
	ctx      *forge.RunContext
}

// FunctionArgs mirrors the SST aws.Function args, translated to Go.
type FunctionArgs struct {
	// Handler is the entrypoint — e.g. "bootstrap" for Go or "src/index.handler" for Node.
	Handler string
	// Runtime defaults to "provided.al2023" (Go compiled binary).
	Runtime string
	// Architecture defaults to "arm64" (Graviton — cheaper & faster for Go).
	Architecture string
	// Timeout in seconds. Defaults to 10.
	Timeout int
	// MemorySize in MB. Defaults to 128.
	MemorySize int
	// Environment variables. Merged with variables from linked resources.
	Environment map[string]string
	// Link injects ARNs / URLs from other constructs as environment variables.
	Link []forge.Linkable
	// URL, if true, creates a Lambda Function URL (no API Gateway needed).
	URL bool
	// Description is an optional human-readable description.
	Description string
	// Code is the path to a pre-built zip file containing the Lambda deployment package.
	// For Go: build with GOARCH=arm64 GOOS=linux go build -o bootstrap, then zip the binary.
	// If empty, the Lambda is registered without code — deploy code separately (e.g. via CI).
	Code string
	// DevHandler is the Go package path (relative to project root) to run locally in dev mode.
	// Example: "./functions/api". When set, `forge dev` builds this package and routes Lambda
	// invocations to the local binary via the SQS tunnel instead of running real Lambda code.
	DevHandler string
	// KMSKeyArn is the ARN of a customer-managed KMS key used to encrypt the function's
	// environment variables. A kms:Grant is created automatically for the execution role.
	// The key policy must also allow the CloudWatch Logs service principal to use the key
	// if log group encryption is desired.
	KMSKeyArn pulumi.StringInput
	// LogRetentionDays sets CloudWatch log retention. 0 = default (14 days), -1 = never expire.
	// Valid non-zero values: 1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731,
	// 1096, 1827, 2192, 2557, 2922, 3288, 3653.
	LogRetentionDays int
	// VpcSubnetIDs places the Lambda inside a VPC. Required when EfsMount is set.
	// The execution role automatically receives AWSLambdaVPCAccessExecutionRole.
	VpcSubnetIDs []string
	// VpcSecurityGroupIDs restricts Lambda VPC egress. Provide alongside VpcSubnetIDs.
	VpcSecurityGroupIDs []string
	// EfsMount wires an EFS access point into the Lambda. VpcSubnetIDs must also be set
	// to subnets that contain the EFS mount targets.
	EfsMount *Efs
}

// NewFunction creates a Lambda function construct.
func NewFunction(ctx *forge.RunContext, name string, args *FunctionArgs) *Function {
	if args == nil {
		args = &FunctionArgs{}
	}
	if args.Runtime == "" {
		args.Runtime = RuntimeGo
	}
	if args.Architecture == "" {
		args.Architecture = ArchARM64
	}
	if args.Timeout == 0 {
		args.Timeout = 10
	}
	if args.MemorySize == 0 {
		args.MemorySize = 128
	}

	if ctx.DevMode {
		return newFunctionDev(ctx, name, args)
	}

	if args.EfsMount != nil && len(args.VpcSubnetIDs) == 0 {
		panic(fmt.Sprintf("NewFunction %q: VpcSubnetIDs is required when EfsMount is set", name))
	}

	pctx := ctx.Pulumi()

	// ── IAM role ─────────────────────────────────────────────────────────────
	managedPolicies := pulumi.StringArray{
		pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
	}
	if len(args.VpcSubnetIDs) > 0 {
		managedPolicies = append(managedPolicies,
			pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaVPCAccessExecutionRole"),
		)
	}
	role, err := iam.NewRole(pctx, name+"-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": { "Service": "lambda.amazonaws.com" },
				"Action": "sts:AssumeRole"
			}]
		}`),
		ManagedPolicyArns: managedPolicies,
		Tags:              defaultTags(ctx, name),
	})
	panicOnErr(err, name+": iam role")

	// ── CloudWatch log group ──────────────────────────────────────────────────
	logGroupArgs := &cloudwatch.LogGroupArgs{
		Name: pulumi.Sprintf("/aws/lambda/%s", qualifiedName(ctx, name)),
		Tags: defaultTags(ctx, name),
	}
	if r := resolveLogRetention(args.LogRetentionDays); r != 0 {
		logGroupArgs.RetentionInDays = pulumi.Int(r)
	}
	if args.KMSKeyArn != nil {
		logGroupArgs.KmsKeyId = args.KMSKeyArn
	}
	_, err = cloudwatch.NewLogGroup(pctx, name+"-logs", logGroupArgs)
	panicOnErr(err, name+": log group")

	// ── Merge environment variables ───────────────────────────────────────────
	envVars := pulumi.StringMap{}
	for k, v := range args.Environment {
		envVars[k] = pulumi.String(v)
	}
	// Inject linked resources' env vars (URLs, ARNs, table names, etc.)
	for _, link := range args.Link {
		for k, v := range link.LinkEnv() {
			envVars[k] = v
		}
	}
	// Always inject stage so handlers know their environment.
	envVars["FORGE_STAGE"] = pulumi.String(ctx.Stage)

	// ── Lambda function ───────────────────────────────────────────────────────
	tracingMode := "PassThrough"
	if !ctx.DevMode {
		tracingMode = "Active" // X-Ray in non-dev
	}

	fnArgs := &awslambda.FunctionArgs{
		Name:          pulumi.String(qualifiedName(ctx, name)),
		Role:          role.Arn,
		Handler:       pulumi.String(args.Handler),
		Runtime:       pulumi.String(args.Runtime),
		Architectures: pulumi.StringArray{pulumi.String(args.Architecture)},
		Timeout:       pulumi.Int(args.Timeout),
		MemorySize:    pulumi.Int(args.MemorySize),
		TracingConfig: &awslambda.FunctionTracingConfigArgs{
			Mode: pulumi.String(tracingMode),
		},
		Tags: defaultTags(ctx, name),
	}

	if len(envVars) > 0 {
		fnArgs.Environment = &awslambda.FunctionEnvironmentArgs{
			Variables: envVars,
		}
	}

	if args.Description != "" {
		fnArgs.Description = pulumi.String(args.Description)
	}
	if args.Code != "" {
		fnArgs.Code = pulumi.NewFileArchive(resolvePath(ctx, args.Code))
	}
	if args.KMSKeyArn != nil {
		fnArgs.KmsKeyArn = args.KMSKeyArn
	}
	if len(args.VpcSubnetIDs) > 0 {
		subnetIDs := make(pulumi.StringArray, len(args.VpcSubnetIDs))
		for i, id := range args.VpcSubnetIDs {
			subnetIDs[i] = pulumi.String(id)
		}
		sgIDs := make(pulumi.StringArray, len(args.VpcSecurityGroupIDs))
		for i, id := range args.VpcSecurityGroupIDs {
			sgIDs[i] = pulumi.String(id)
		}
		fnArgs.VpcConfig = &awslambda.FunctionVpcConfigArgs{
			SubnetIds:        subnetIDs,
			SecurityGroupIds: sgIDs,
		}
	}
	if args.EfsMount != nil {
		fnArgs.FileSystemConfig = &awslambda.FunctionFileSystemConfigArgs{
			Arn:            args.EfsMount.AccessPointARN(),
			LocalMountPath: pulumi.String(args.EfsMount.MountPath()),
		}
	}

	fn, err := awslambda.NewFunction(pctx, name, fnArgs)
	panicOnErr(err, name+": lambda function")

	// Grant the execution role KMS permissions when a customer-managed key is provided.
	if args.KMSKeyArn != nil {
		kmsGrant(pctx, name, args.KMSKeyArn, role.Arn)
	}

	// ── Optional Function URL ─────────────────────────────────────────────────
	if args.URL {
		_, err = awslambda.NewFunctionUrl(pctx, name+"-url", &awslambda.FunctionUrlArgs{
			FunctionName:      fn.Name,
			AuthorizationType: pulumi.String("NONE"),
			Cors: &awslambda.FunctionUrlCorsArgs{
				AllowOrigins: pulumi.StringArray{pulumi.String("*")},
				AllowMethods: pulumi.StringArray{pulumi.String("*")},
				AllowHeaders: pulumi.StringArray{pulumi.String("*")},
			},
		})
		panicOnErr(err, name+": function url")
	}

	return &Function{name: name, resource: fn, role: role, ctx: ctx}
}

// ARN returns the Lambda function ARN as a Pulumi output.
func (f *Function) ARN() pulumi.StringOutput { return f.resource.Arn }

// Role returns the IAM execution role, allowing other constructs to attach policies.
func (f *Function) Role() *iam.Role { return f.role }

// Name returns the physical function name as a Pulumi output.
func (f *Function) Name() pulumi.StringOutput { return f.resource.Name }

// LinkEnv implements Linkable — exposes the function ARN to other constructs.
func (f *Function) LinkEnv() pulumi.StringMap {
	return pulumi.StringMap{
		fmt.Sprintf("SST_FUNCTION_%s_ARN", envKey(f.name)): f.resource.Arn,
	}
}

// LinkName implements Linkable.
func (f *Function) LinkName() string { return f.name }

// ── Dev mode ──────────────────────────────────────────────────────────────────

// newFunctionDev deploys a forge-stub Lambda that proxies invocations over SQS
// to the local `forge dev` tunnel. Called by NewFunction when ctx.DevMode is true.
//
// Shared SQS request/response queues are created once per stack (via ctx.SetDevQueues)
// and reused across all dev functions. The function ARN and local handler source path
// are exported as stack outputs so the CLI can wire up the tunnel.
func newFunctionDev(ctx *forge.RunContext, name string, args *FunctionArgs) *Function {
	pctx := ctx.Pulumi()

	// ── IAM role (same as prod) ───────────────────────────────────────────────
	role, err := iam.NewRole(pctx, name+"-role", &iam.RoleArgs{
		AssumeRolePolicy: pulumi.String(`{
			"Version": "2012-10-17",
			"Statement": [{
				"Effect": "Allow",
				"Principal": { "Service": "lambda.amazonaws.com" },
				"Action": "sts:AssumeRole"
			}]
		}`),
		ManagedPolicyArns: pulumi.StringArray{
			pulumi.String("arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"),
		},
		Tags: defaultTags(ctx, name),
	})
	panicOnErr(err, name+": dev iam role")

	// ── CloudWatch log group (same as prod) ───────────────────────────────────
	logGroupArgs := &cloudwatch.LogGroupArgs{
		Name: pulumi.Sprintf("/aws/lambda/%s", qualifiedName(ctx, name)),
		Tags: defaultTags(ctx, name),
	}
	if r := resolveLogRetention(args.LogRetentionDays); r != 0 {
		logGroupArgs.RetentionInDays = pulumi.Int(r)
	}
	_, err = cloudwatch.NewLogGroup(pctx, name+"-logs", logGroupArgs)
	panicOnErr(err, name+": dev log group")

	// ── Shared dev SQS queues (created once per stack) ────────────────────────
	reqURL, resURL, queuesExist := ctx.DevQueues()
	if !queuesExist {
		reqQ, qErr := sqs.NewQueue(pctx, "forge-dev-req", &sqs.QueueArgs{
			Name:                     pulumi.Sprintf("%s-%s-forge-dev-req", ctx.App.Name, ctx.Stage),
			VisibilityTimeoutSeconds: pulumi.Int(30),
			MessageRetentionSeconds:  pulumi.Int(60),
		})
		panicOnErr(qErr, "forge-dev-req queue")

		resQ, qErr := sqs.NewQueue(pctx, "forge-dev-res", &sqs.QueueArgs{
			Name:                     pulumi.Sprintf("%s-%s-forge-dev-res", ctx.App.Name, ctx.Stage),
			VisibilityTimeoutSeconds: pulumi.Int(5),
			MessageRetentionSeconds:  pulumi.Int(60),
		})
		panicOnErr(qErr, "forge-dev-res queue")

		ctx.SetDevQueues(reqQ.Url, resQ.Url)
		reqURL, resURL, _ = ctx.DevQueues()
	}

	// ── SQS access policy for stub Lambda ─────────────────────────────────────
	// The stub needs to send to the request queue and poll the response queue.
	sqsPolicy := pulumi.All(reqURL, resURL).ApplyT(func(vals []interface{}) (string, error) {
		req := fmt.Sprint(vals[0])
		res := fmt.Sprint(vals[1])
		// Convert queue URL to ARN: https://sqs.<region>.amazonaws.com/<account>/<name>
		// For Nimbus / LocalStack the URL format is identical so we derive the ARN.
		return fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Action": ["sqs:SendMessage"],
					"Resource": "%s"
				},
				{
					"Effect": "Allow",
					"Action": ["sqs:ReceiveMessage","sqs:DeleteMessage","sqs:ChangeMessageVisibility"],
					"Resource": "%s"
				}
			]
		}`, sqsURLtoARN(req), sqsURLtoARN(res)), nil
	}).(pulumi.StringOutput)

	_, err = iam.NewRolePolicy(pctx, name+"-dev-sqs", &iam.RolePolicyArgs{
		Role:   role.Name,
		Policy: sqsPolicy,
	})
	panicOnErr(err, name+": dev sqs policy")

	// ── Merge environment variables ───────────────────────────────────────────
	envVars := pulumi.StringMap{
		"FORGE_STAGE":              pulumi.String(ctx.Stage),
		"FORGE_REQUEST_QUEUE_URL":  reqURL,
		"FORGE_RESPONSE_QUEUE_URL": resURL,
	}
	for k, v := range args.Environment {
		envVars[k] = pulumi.String(v)
	}
	for _, link := range args.Link {
		for k, v := range link.LinkEnv() {
			envVars[k] = v
		}
	}

	// ── Stub Lambda (x86_64 linux, 30 s timeout to match poll window) ─────────
	fnArgs := &awslambda.FunctionArgs{
		Name:          pulumi.String(qualifiedName(ctx, name)),
		Role:          role.Arn,
		Handler:       pulumi.String("bootstrap"),
		Runtime:       pulumi.String(RuntimeGo),
		Architectures: pulumi.StringArray{pulumi.String(ArchX8664)},
		Timeout:       pulumi.Int(30),
		MemorySize:    pulumi.Int(128),
		Environment: &awslambda.FunctionEnvironmentArgs{
			Variables: envVars,
		},
		Tags: defaultTags(ctx, name),
	}
	if stubZip := os.Getenv("FORGE_STUB_ZIP"); stubZip != "" {
		fnArgs.Code = pulumi.NewFileArchive(stubZip)
	}

	fn, err := awslambda.NewFunction(pctx, name, fnArgs)
	panicOnErr(err, name+": stub lambda function")

	// ── Export ARN + handler source for the CLI tunnel ────────────────────────
	pctx.Export("devHandlerArn_"+name, fn.Arn)
	pctx.Export("devHandlerSrc_"+name, pulumi.String(args.DevHandler))

	return &Function{name: name, resource: fn, role: role, ctx: ctx}
}

// sqsURLtoARN converts an SQS queue URL to its ARN.
// URL format: https://sqs.<region>.amazonaws.com/<account>/<name>
// Also handles Nimbus/LocalStack: http://localhost:4566/000000000000/<name>
func sqsURLtoARN(url string) string {
	// Strip scheme and split on /
	s := url
	for _, prefix := range []string{"https://", "http://"} {
		s = strings.TrimPrefix(s, prefix)
	}
	parts := strings.SplitN(s, "/", 3)
	if len(parts) < 3 {
		return url // can't parse — return as-is
	}
	host := parts[0]  // sqs.us-east-1.amazonaws.com or localhost:4566
	acct := parts[1]  // 123456789012
	qname := parts[2] // queue-name

	// Derive region from host.
	region := "us-east-1"
	hostParts := strings.Split(host, ".")
	if len(hostParts) >= 4 && hostParts[0] == "sqs" {
		region = hostParts[1]
	}
	return fmt.Sprintf("arn:aws:sqs:%s:%s:%s", region, acct, qname)
}
