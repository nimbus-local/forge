package constructs

import (
	"fmt"
	"strings"

	forge "github.com/sst-go/forge"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ApiGatewayV2 creates an AWS HTTP API (API Gateway v2) with routes backed by
// Lambda functions. It mirrors sst.aws.ApiGatewayV2.
type ApiGatewayV2 struct {
	name     string
	api      *apigatewayv2.Api
	stage    *apigatewayv2.Stage
	ctx      *forge.RunContext
	routeIdx int
}

// ApiGatewayV2Args configures the HTTP API.
type ApiGatewayV2Args struct {
	// CorsAllowOrigins defaults to ["*"]. Set to nil to disable CORS.
	CorsAllowOrigins []string
	// CorsAllowMethods defaults to all methods.
	CorsAllowMethods []string
	// CorsAllowHeaders defaults to ["*"].
	CorsAllowHeaders []string
	// AccessLog enables CloudWatch access logging.
	AccessLog bool
}

// RouteArgs configures a single route handler.
type RouteArgs struct {
	// Handler is the Lambda entrypoint (e.g. "functions/api/main.handler").
	Handler string
	// Function is an existing Function construct. Use either Handler or Function.
	Function *Function
	// Link injects linked resource env vars into the route's Lambda.
	Link []forge.Linkable
	// Timeout for this specific route's Lambda. Defaults to FunctionArgs.Timeout.
	Timeout int
	// MemorySize for this route's Lambda. Defaults to FunctionArgs.MemorySize.
	MemorySize int
}

// NewApiGatewayV2 creates an HTTP API construct.
func NewApiGatewayV2(ctx *forge.RunContext, name string, args *ApiGatewayV2Args) *ApiGatewayV2 {
	if args == nil {
		args = &ApiGatewayV2Args{
			CorsAllowOrigins: []string{"*"},
			CorsAllowMethods: []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS"},
			CorsAllowHeaders: []string{"*"},
		}
	}

	pctx := ctx.Pulumi()

	corsOrigins := toStringArray(args.CorsAllowOrigins)
	corsMethods := toStringArray(args.CorsAllowMethods)
	corsHeaders := toStringArray(args.CorsAllowHeaders)

	api, err := apigatewayv2.NewApi(pctx, name, &apigatewayv2.ApiArgs{
		Name:         pulumi.String(qualifiedName(ctx, name)),
		ProtocolType: pulumi.String("HTTP"),
		CorsConfiguration: &apigatewayv2.ApiCorsConfigurationArgs{
			AllowOrigins: corsOrigins,
			AllowMethods: corsMethods,
			AllowHeaders: corsHeaders,
		},
		Tags: defaultTags(ctx, name),
	})
	panicOnErr(err, name+": api gateway")

	// Auto-deploy stage — mirrors SST's default behaviour.
	stage, err := apigatewayv2.NewStage(pctx, name+"-stage", &apigatewayv2.StageArgs{
		ApiId:      api.ID(),
		Name:       pulumi.String("$default"),
		AutoDeploy: pulumi.Bool(true),
		Tags:       defaultTags(ctx, name),
	})
	panicOnErr(err, name+": stage")

	return &ApiGatewayV2{name: name, api: api, stage: stage, ctx: ctx}
}

// Route adds a Lambda-backed route to the API.
// routeKey format: "METHOD /path" e.g. "GET /users/{id}" or "ANY /{proxy+}".
// rArgs can be a *RouteArgs or an existing *Function.
func (a *ApiGatewayV2) Route(routeKey string, rArgs *RouteArgs) *ApiGatewayV2 {
	a.routeIdx++
	safeName := routeSafeName(routeKey, a.routeIdx)
	pctx := a.ctx.Pulumi()

	var fn *Function
	if rArgs.Function != nil {
		fn = rArgs.Function
	} else {
		fn = NewFunction(a.ctx, a.name+"-"+safeName, &FunctionArgs{
			Handler:    rArgs.Handler,
			Link:       rArgs.Link,
			Timeout:    rArgs.Timeout,
			MemorySize: rArgs.MemorySize,
		})
	}

	// Allow API Gateway to invoke the Lambda.
	_, err := lambda.NewPermission(pctx, a.name+"-"+safeName+"-perm", &lambda.PermissionArgs{
		Action:    pulumi.String("lambda:InvokeFunction"),
		Function:  fn.resource.Name,
		Principal: pulumi.String("apigateway.amazonaws.com"),
		SourceArn: pulumi.Sprintf("%s/*/*", a.api.ExecutionArn),
	})
	panicOnErr(err, a.name+": lambda permission for "+routeKey)

	// Lambda integration.
	integration, err := apigatewayv2.NewIntegration(pctx, a.name+"-"+safeName+"-int", &apigatewayv2.IntegrationArgs{
		ApiId:                a.api.ID(),
		IntegrationType:      pulumi.String("AWS_PROXY"),
		IntegrationUri:       fn.resource.Arn,
		PayloadFormatVersion: pulumi.String("2.0"),
	})
	panicOnErr(err, a.name+": integration for "+routeKey)

	// Route.
	_, err = apigatewayv2.NewRoute(pctx, a.name+"-"+safeName+"-route", &apigatewayv2.RouteArgs{
		ApiId:    a.api.ID(),
		RouteKey: pulumi.String(routeKey),
		Target:   pulumi.Sprintf("integrations/%s", integration.ID()),
	})
	panicOnErr(err, a.name+": route "+routeKey)

	return a // fluent
}

// URL returns the API's invoke URL as a Pulumi output.
func (a *ApiGatewayV2) URL() pulumi.StringOutput {
	return a.stage.InvokeUrl
}

// LinkEnv implements Linkable.
func (a *ApiGatewayV2) LinkEnv() pulumi.StringMap {
	return pulumi.StringMap{
		fmt.Sprintf("SST_API_%s_URL", envKey(a.name)): a.stage.InvokeUrl,
	}
}
func (a *ApiGatewayV2) LinkName() string { return a.name }

// ── helpers ───────────────────────────────────────────────────────────────────

func routeSafeName(routeKey string, idx int) string {
	// "GET /users/{id}" → "get-users-id-1"
	s := strings.ToLower(routeKey)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "{", "")
	s = strings.ReplaceAll(s, "}", "")
	s = strings.Trim(s, "-")
	return fmt.Sprintf("%s-%d", s, idx)
}

func toStringArray(ss []string) pulumi.StringArray {
	out := make(pulumi.StringArray, len(ss))
	for i, s := range ss {
		out[i] = pulumi.String(s)
	}
	return out
}

// addApiInvokePolicy adds an inline policy to a role allowing it to call the API.
func addApiInvokePolicy(pctx *pulumi.Context, name string, role *iam.Role, apiArn pulumi.StringOutput) {
	_, err := iam.NewRolePolicy(pctx, name+"-invoke-policy", &iam.RolePolicyArgs{
		Role: role.Name,
		Policy: apiArn.ApplyT(func(arn string) string {
			return fmt.Sprintf(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Action": "execute-api:Invoke",
					"Resource": "%s/*"
				}]
			}`, arn)
		}).(pulumi.StringOutput),
	})
	panicOnErr(err, name+": api invoke policy")
}
