package constructs

import (
	"testing"

	forge "github.com/nimbus-local/forge"
)

func TestNewApiGatewayWebSocket_ApiCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	if mocks.find("aws:apigatewayv2/api:Api") == nil {
		t.Error("WebSocket API not created")
	}
}

func TestNewApiGatewayWebSocket_ProtocolTypeWebSocket(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	r := mocks.find("aws:apigatewayv2/api:Api")
	if r == nil {
		t.Fatal("API not registered")
	}
	if r.inputs["protocolType"].StringValue() != "WEBSOCKET" {
		t.Errorf("protocolType = %q, want WEBSOCKET", r.inputs["protocolType"].StringValue())
	}
}

func TestNewApiGatewayWebSocket_RouteSelectionExpression(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	r := mocks.find("aws:apigatewayv2/api:Api")
	if r == nil {
		t.Fatal("API not registered")
	}
	if r.inputs["routeSelectionExpression"].StringValue() != "$request.body.action" {
		t.Errorf("routeSelectionExpression = %q, want $request.body.action",
			r.inputs["routeSelectionExpression"].StringValue())
	}
}

func TestNewApiGatewayWebSocket_PhysicalNameQualified(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	r := mocks.find("aws:apigatewayv2/api:Api")
	if r == nil {
		t.Fatal("API not registered")
	}
	if r.inputs["name"].StringValue() != "myapp-test-Chat" {
		t.Errorf("api name = %q, want myapp-test-Chat", r.inputs["name"].StringValue())
	}
}

func TestNewApiGatewayWebSocket_StageCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	if mocks.find("aws:apigatewayv2/stage:Stage") == nil {
		t.Error("Stage not created")
	}
}

func TestNewApiGatewayWebSocket_StageIsDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	r := mocks.find("aws:apigatewayv2/stage:Stage")
	if r == nil {
		t.Fatal("Stage not registered")
	}
	if r.inputs["name"].StringValue() != "$default" {
		t.Errorf("stage name = %q, want $default", r.inputs["name"].StringValue())
	}
}

func TestNewApiGatewayWebSocket_TagsApplied(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	r := mocks.find("aws:apigatewayv2/api:Api")
	if r == nil {
		t.Fatal("API not registered")
	}
	for _, tag := range []string{"forge:app", "forge:stage", "forge:name"} {
		assertTag(t, r.inputs, tag)
	}
}

func TestNewApiGatewayWebSocket_NilArgsSafe(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	if mocks.find("aws:apigatewayv2/api:Api") == nil {
		t.Error("API not created with nil args")
	}
}

func TestNewApiGatewayWebSocket_NoRoutesByDefault(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil)
	})

	if mocks.find("aws:apigatewayv2/route:Route") != nil {
		t.Error("Route should not be created with no routes")
	}
}

func TestNewApiGatewayWebSocket_RouteCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", &ApiGatewayWebSocketArgs{
			Routes: map[string]*FunctionArgs{
				"$connect": {Handler: "bootstrap"},
			},
		})
	})

	if mocks.find("aws:apigatewayv2/route:Route") == nil {
		t.Error("Route not created")
	}
	if mocks.find("aws:apigatewayv2/integration:Integration") == nil {
		t.Error("Integration not created")
	}
	if mocks.find("aws:lambda/function:Function") == nil {
		t.Error("Lambda function not created")
	}
}

func TestNewApiGatewayWebSocket_LambdaPermissionCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", &ApiGatewayWebSocketArgs{
			Routes: map[string]*FunctionArgs{
				"$connect": {Handler: "bootstrap"},
			},
		})
	})

	if mocks.find("aws:lambda/permission:Permission") == nil {
		t.Error("Lambda invoke permission not created")
	}
}

func TestNewApiGatewayWebSocket_ManageConnectionsPolicyCreated(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", &ApiGatewayWebSocketArgs{
			Routes: map[string]*FunctionArgs{
				"$connect": {Handler: "bootstrap"},
			},
		})
	})

	if mocks.find("aws:iam/rolePolicy:RolePolicy") == nil {
		t.Error("ManageConnections IAM policy not created")
	}
}

func TestNewApiGatewayWebSocket_IntegrationMethodIsPost(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", &ApiGatewayWebSocketArgs{
			Routes: map[string]*FunctionArgs{
				"$default": {Handler: "bootstrap"},
			},
		})
	})

	r := mocks.find("aws:apigatewayv2/integration:Integration")
	if r == nil {
		t.Fatal("Integration not registered")
	}
	if r.inputs["integrationMethod"].StringValue() != "POST" {
		t.Errorf("integrationMethod = %q, want POST", r.inputs["integrationMethod"].StringValue())
	}
}

func TestNewApiGatewayWebSocket_MultipleRoutes(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", &ApiGatewayWebSocketArgs{
			Routes: map[string]*FunctionArgs{
				"$connect":    {Handler: "bootstrap"},
				"$disconnect": {Handler: "bootstrap"},
				"$default":    {Handler: "bootstrap"},
			},
		})
	})

	routes := mocks.findAll("aws:apigatewayv2/route:Route")
	if len(routes) != 3 {
		t.Errorf("expected 3 routes, got %d", len(routes))
	}
	lambdas := mocks.findAll("aws:lambda/function:Function")
	if len(lambdas) != 3 {
		t.Errorf("expected 3 Lambda functions, got %d", len(lambdas))
	}
}

func TestNewApiGatewayWebSocket_FluentRouteMethod(t *testing.T) {
	t.Parallel()
	mocks := runTest(t, func(ctx *forge.RunContext) {
		NewApiGatewayWebSocket(ctx, "Chat", nil).
			Route("$connect", &FunctionArgs{Handler: "bootstrap"}).
			Route("$disconnect", &FunctionArgs{Handler: "bootstrap"})
	})

	routes := mocks.findAll("aws:apigatewayv2/route:Route")
	if len(routes) != 2 {
		t.Errorf("expected 2 routes via fluent API, got %d", len(routes))
	}
}

func TestNewApiGatewayWebSocket_LinkEnvKeys(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		ws := NewApiGatewayWebSocket(ctx, "Chat", nil)
		env := ws.LinkEnv()
		if _, ok := env["SST_WS_CHAT_URL"]; !ok {
			t.Error("LinkEnv missing SST_WS_CHAT_URL")
		}
		if _, ok := env["SST_WS_CHAT_MGMT_URL"]; !ok {
			t.Error("LinkEnv missing SST_WS_CHAT_MGMT_URL")
		}
		if len(env) != 2 {
			t.Errorf("LinkEnv has %d keys, want 2", len(env))
		}
	})
}

func TestNewApiGatewayWebSocket_CamelCaseLinkEnvKey(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		ws := NewApiGatewayWebSocket(ctx, "ChatServer", nil)
		env := ws.LinkEnv()
		if _, ok := env["SST_WS_CHAT_SERVER_URL"]; !ok {
			t.Error("LinkEnv missing SST_WS_CHAT_SERVER_URL")
		}
	})
}

func TestNewApiGatewayWebSocket_LinkName(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		ws := NewApiGatewayWebSocket(ctx, "Chat", nil)
		if ws.LinkName() != "Chat" {
			t.Errorf("LinkName = %q, want Chat", ws.LinkName())
		}
	})
}

func TestNewApiGatewayWebSocket_ImplementsLinkable(t *testing.T) {
	t.Parallel()
	runTest(t, func(ctx *forge.RunContext) {
		ws := NewApiGatewayWebSocket(ctx, "Chat", nil)
		var _ forge.Linkable = ws
	})
}

func TestNewApiGatewayWebSocket_WsRouteSafeName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"$connect", "connect"},
		{"$disconnect", "disconnect"},
		{"$default", "default"},
		{"sendMessage", "sendmessage"},
		{"send_message", "send-message"},
	}
	for _, tc := range cases {
		got := wsRouteSafeName(tc.in)
		if got != tc.want {
			t.Errorf("wsRouteSafeName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
