# ApiGatewayV2

Creates an AWS API Gateway HTTP API (v2) with a default stage and auto-deploy enabled.

```go
import "github.com/nimbus-local/forge/constructs"

api := constructs.NewApiGatewayV2(ctx, "Api", nil)
api.Route("GET /", &constructs.RouteArgs{Function: fn})

ctx.Export("url", api.URL())
```

---

## NewApiGatewayV2

```go
func NewApiGatewayV2(ctx *forge.RunContext, name string, args *ApiGatewayV2Args) *ApiGatewayV2
```

`args` may be `nil` — all fields have defaults.

### ApiGatewayV2Args

| Field | Type | Default | Description |
|---|---|---|---|
| `CorsAllowOrigins` | `[]string` | `["*"]` | CORS allowed origins |
| `CorsAllowMethods` | `[]string` | `["*"]` | CORS allowed HTTP methods |
| `CorsAllowHeaders` | `[]string` | `["*"]` | CORS allowed request headers |

---

## Route

```go
func (a *ApiGatewayV2) Route(pattern string, args *RouteArgs)
```

Adds an HTTP route. `pattern` is `"METHOD /path"` — e.g. `"GET /users"`, `"POST /orders/{id}"`, `"$default"`.

### RouteArgs

| Field | Type | Description |
|---|---|---|
| `Function` | `*Function` | Lambda function that handles the route |

---

## Methods

```go
func (a *ApiGatewayV2) URL() pulumi.StringOutput  // HTTPS invoke URL
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_API_<NAME>_URL` | API Gateway invoke URL |

---

## Example — REST API with multiple routes

```go
fn := constructs.NewFunction(ctx, "Handler", &constructs.FunctionArgs{
    Handler: "bootstrap",
})

api := constructs.NewApiGatewayV2(ctx, "Api", &constructs.ApiGatewayV2Args{
    CorsAllowOrigins: []string{"https://example.com"},
})

api.Route("GET /users",       &constructs.RouteArgs{Function: fn})
api.Route("POST /users",      &constructs.RouteArgs{Function: fn})
api.Route("DELETE /users/{id}", &constructs.RouteArgs{Function: fn})

ctx.Export("url", api.URL())
```
