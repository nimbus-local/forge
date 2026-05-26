package constructs

import (
	"fmt"
	"strings"

	forge "github.com/nimbus-local/forge"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/apigatewayv2"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/iam"
	"github.com/pulumi/pulumi-aws/sdk/v7/go/aws/lambda"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)

// ApiGatewayWebSocket creates an AWS WebSocket API (API Gateway v2) with Lambda-backed
// route handlers. It mirrors sst.aws.ApiGatewayWebSocket.
//
// Routes are matched via the RouteSelectionExpression "$request.body.action".
// Built-in routes: "$connect", "$disconnect", "$default".
// Each route's Lambda is automatically granted execute-api:ManageConnections so it can
// push messages back to connected clients.
type ApiGatewayWebSocket struct {
	name  string
	api   *apigatewayv2.Api
	stage *apigatewayv2.Stage
	ctx   *forge.RunContext
}

// ApiGatewayWebSocketArgs configures the WebSocket API.
type ApiGatewayWebSocketArgs struct {
	// Routes maps route key to handler configuration.
	// Built-in keys: "$connect", "$disconnect", "$default".
	// Custom keys are matched when $request.body.action equals the key.
	Routes map[string]*FunctionArgs
}

// NewApiGatewayWebSocket creates a WebSocket API construct.
func NewApiGatewayWebSocket(ctx *forge.RunContext, name string, args *ApiGatewayWebSocketArgs) *ApiGatewayWebSocket {
	if args == nil {
		args = &ApiGatewayWebSocketArgs{}
	}

	pctx := ctx.Pulumi()

	api, err := apigatewayv2.NewApi(pctx, name, &apigatewayv2.ApiArgs{
		Name:                     pulumi.String(qualifiedName(ctx, name)),
		ProtocolType:             pulumi.String("WEBSOCKET"),
		RouteSelectionExpression: pulumi.String("$request.body.action"),
		Tags:                     defaultTags(ctx, name),
	})
	panicOnErr(err, name+": websocket api")

	stage, err := apigatewayv2.NewStage(pctx, name+"-stage", &apigatewayv2.StageArgs{
		ApiId:      api.ID(),
		Name:       pulumi.String("$default"),
		AutoDeploy: pulumi.Bool(true),
		Tags:       defaultTags(ctx, name),
	})
	panicOnErr(err, name+": stage")

	ws := &ApiGatewayWebSocket{name: name, api: api, stage: stage, ctx: ctx}

	for routeKey, fnArgs := range args.Routes {
		ws.addRoute(routeKey, fnArgs)
	}

	return ws
}

// Route adds a Lambda-backed route after construction (fluent API).
func (ws *ApiGatewayWebSocket) Route(routeKey string, fnArgs *FunctionArgs) *ApiGatewayWebSocket {
	ws.addRoute(routeKey, fnArgs)
	return ws
}

func (ws *ApiGatewayWebSocket) addRoute(routeKey string, fnArgs *FunctionArgs) {
	safeName := wsRouteSafeName(routeKey)
	pctx := ws.ctx.Pulumi()

	fn := NewFunction(ws.ctx, ws.name+"-"+safeName, fnArgs)

	// Grant the Lambda permission to push messages to connected clients.
	_, err := iam.NewRolePolicy(pctx, ws.name+"-"+safeName+"-mgmt", &iam.RolePolicyArgs{
		Role: fn.Role().Name,
		Policy: ws.api.ExecutionArn.ApplyT(func(arn string) string {
			return fmt.Sprintf(`{
				"Version": "2012-10-17",
				"Statement": [{
					"Effect": "Allow",
					"Action": "execute-api:ManageConnections",
					"Resource": "%s/*"
				}]
			}`, arn)
		}).(pulumi.StringOutput),
	})
	panicOnErr(err, ws.name+": manage connections policy for "+routeKey)

	_, err = lambda.NewPermission(pctx, ws.name+"-"+safeName+"-perm", &lambda.PermissionArgs{
		Action:    pulumi.String("lambda:InvokeFunction"),
		Function:  fn.resource.Name,
		Principal: pulumi.String("apigateway.amazonaws.com"),
		SourceArn: pulumi.Sprintf("%s/*", ws.api.ExecutionArn),
	})
	panicOnErr(err, ws.name+": lambda permission for "+routeKey)

	integration, err := apigatewayv2.NewIntegration(pctx, ws.name+"-"+safeName+"-int", &apigatewayv2.IntegrationArgs{
		ApiId:             ws.api.ID(),
		IntegrationType:   pulumi.String("AWS_PROXY"),
		IntegrationUri:    fn.resource.Arn,
		IntegrationMethod: pulumi.String("POST"),
	})
	panicOnErr(err, ws.name+": integration for "+routeKey)

	_, err = apigatewayv2.NewRoute(pctx, ws.name+"-"+safeName+"-route", &apigatewayv2.RouteArgs{
		ApiId:    ws.api.ID(),
		RouteKey: pulumi.String(routeKey),
		Target:   pulumi.Sprintf("integrations/%s", integration.ID()),
	})
	panicOnErr(err, ws.name+": route "+routeKey)
}

// URL returns the WebSocket connection URL (wss://) as a Pulumi output.
func (ws *ApiGatewayWebSocket) URL() pulumi.StringOutput {
	return ws.stage.InvokeUrl
}

// MgmtURL returns the HTTPS management URL used to post messages to connected clients.
func (ws *ApiGatewayWebSocket) MgmtURL() pulumi.StringOutput {
	return ws.stage.InvokeUrl.ApplyT(func(url string) string {
		return strings.Replace(url, "wss://", "https://", 1)
	}).(pulumi.StringOutput)
}

// LinkEnv implements Linkable.
func (ws *ApiGatewayWebSocket) LinkEnv() pulumi.StringMap {
	key := envKey(ws.name)
	return pulumi.StringMap{
		fmt.Sprintf("SST_WS_%s_URL", key):      ws.stage.InvokeUrl,
		fmt.Sprintf("SST_WS_%s_MGMT_URL", key): ws.MgmtURL(),
	}
}

// LinkName implements Linkable.
func (ws *ApiGatewayWebSocket) LinkName() string { return ws.name }

// ── helpers ───────────────────────────────────────────────────────────────────

// wsRouteSafeName converts a WebSocket route key to a resource-name-safe string.
// "$connect" → "connect", "$default" → "default", "sendMessage" → "sendmessage"
func wsRouteSafeName(routeKey string) string {
	s := strings.TrimPrefix(routeKey, "$")
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	return s
}
