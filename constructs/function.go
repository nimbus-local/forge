package constructs

import (
	"fmt"

	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/cloudwatch"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	awslambda "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	forge "github.com/nimbus-local/forge"
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
}

// NewFunction creates a Lambda function construct.
func NewFunction(ctx *forge.RunContext, name string, args *FunctionArgs) *Function {
	if args == nil {
		args = &FunctionArgs{}
	}
	if args.Runtime == "" {
		args.Runtime = "provided.al2023"
	}
	if args.Architecture == "" {
		args.Architecture = "arm64"
	}
	if args.Timeout == 0 {
		args.Timeout = 10
	}
	if args.MemorySize == 0 {
		args.MemorySize = 128
	}

	pctx := ctx.Pulumi()

	// ── IAM role ─────────────────────────────────────────────────────────────
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
	panicOnErr(err, name+": iam role")

	// ── CloudWatch log group ──────────────────────────────────────────────────
	_, err = cloudwatch.NewLogGroup(pctx, name+"-logs", &cloudwatch.LogGroupArgs{
		Name:            pulumi.Sprintf("/aws/lambda/%s", name),
		RetentionInDays: pulumi.Int(14),
		Tags:            defaultTags(ctx, name),
	})
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
		fnArgs.Code = pulumi.NewFileArchive(args.Code)
	}

	fn, err := awslambda.NewFunction(pctx, name, fnArgs)
	panicOnErr(err, name+": lambda function")

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
