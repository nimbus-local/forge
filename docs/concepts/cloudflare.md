# Cloudflare Support

forge can deploy to Cloudflare alongside AWS (or instead of it). Set `App.Home` to `"cloudflare"` or `"aws+cloudflare"` to activate Cloudflare resource support.

---

## Configuration

Add `Cloudflare` to your `AppConfig`:

```go
forge.Run(&forge.Config{
    App: &forge.AppConfig{
        Name: "my-app",
        Home: "aws+cloudflare",      // "cloudflare" for CF-only projects
        Cloudflare: &forge.CloudflareConfig{
            AccountID: "abc123",     // or CLOUDFLARE_ACCOUNT_ID
            ZoneID:    "xyz789",     // optional; required for custom Worker domains
        },
    },
    Run: func(ctx *forge.RunContext) error {
        // use both AWS and CF constructs here
        return nil
    },
})
```

### Home values

| Value | Plugins installed | Use when |
|---|---|---|
| `"aws"` (default) | AWS v6 | AWS-only project |
| `"cloudflare"` | Cloudflare v5 | CF-only project |
| `"aws+cloudflare"` | AWS v6 + Cloudflare v5 | Mixed AWS + CF project |

---

## Authentication

forge checks for credentials in this order:

1. `CLOUDFLARE_API_TOKEN` — preferred; create one in the [Cloudflare dashboard](https://dash.cloudflare.com/profile/api-tokens) with Workers, KV, D1, and R2 permissions.
2. `CLOUDFLARE_API_KEY` + `CLOUDFLARE_EMAIL` — legacy global API key.

forge will exit with a clear error message if neither is set when Cloudflare is required.

```bash
export CLOUDFLARE_API_TOKEN=your-token-here
export CLOUDFLARE_ACCOUNT_ID=your-account-id
```

---

## Available constructs

Import from `github.com/sst-go/forge/constructs/cloudflare`:

```go
import cf "github.com/sst-go/forge/constructs/cloudflare"
```

| Construct | Description |
|---|---|
| [`NewWorker`](../constructs/cloudflare/worker.md) | Deploy a Workers script (JS/TS or Go WASM) |
| [`NewKVNamespace`](../constructs/cloudflare/kv.md) | Create a Workers KV namespace |
| [`NewD1Database`](../constructs/cloudflare/d1.md) | Create a D1 SQLite database |
| [`NewR2Bucket`](../constructs/cloudflare/r2.md) | Create an R2 object-storage bucket |

---

## Bindings

Cloudflare Workers access KV, D1, and R2 resources through **bindings** — named references injected into the Worker's `env` object at runtime.

forge names bindings using the SCREAMING_SNAKE_CASE of the construct's logical name:

```go
kv := cf.NewKVNamespace(ctx, "Sessions", nil)  // binding: SESSIONS
db := cf.NewD1Database(ctx, "Users", nil)       // binding: USERS
r2 := cf.NewR2Bucket(ctx, "Assets", nil)        // binding: ASSETS

cf.NewWorker(ctx, "Api", &cf.WorkerArgs{
    Handler:    "worker/index.ts",
    KVBindings: []*cf.KVNamespace{kv},
    D1Bindings: []*cf.D1Database{db},
    R2Bindings: []*cf.R2Bucket{r2},
})
```

Inside the Worker:

```ts
export default {
    async fetch(request: Request, env: Env): Promise<Response> {
        const session = await env.SESSIONS.get("user:123");
        const row = await env.USERS.prepare("SELECT * FROM users LIMIT 1").first();
        const obj = await env.ASSETS.get("logo.png");
        return new Response("ok");
    },
};
```

---

## Linking AWS resources to a Worker

Use `WorkerArgs.Link` to inject AWS resource identifiers (ARNs, URLs, names) as plain-text Worker bindings. The same `SST_*` env var naming convention is used as for Lambda:

```go
api   := constructs.NewApiGatewayV2(ctx, "Api", nil)
table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
    PrimaryIndex: constructs.PrimaryIndex{PartitionKey: "id"},
})

worker := cf.NewWorker(ctx, "Frontend", &cf.WorkerArgs{
    Handler: "worker/index.ts",
    Link:    []forge.Linkable{api, table},
})
```

Inside the Worker the values are available as plain-text bindings:

```ts
const apiURL    = env.SST_API_API_URL;
const tableName = env.SST_TABLE_ORDERS_NAME;
```

Conversely, CF constructs implement `forge.Linkable` and can be linked to AWS Lambdas:

```go
kv := cf.NewKVNamespace(ctx, "Cache", nil)

fn := constructs.NewFunction(ctx, "Processor", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{kv},  // injects SST_KV_CACHE_ID and SST_KV_CACHE_NAME
})
```

See [concepts/linking.md](linking.md) for the full linking contract.

---

## Resource naming

All Cloudflare resources are stage-qualified to prevent collisions between stages:

```
<appName>-<stage>-<constructName>

my-app-prod-Sessions     ← KV namespace
my-app-prod-Users        ← D1 database
my-app-prod-Assets       ← R2 bucket
my-app-prod-Api          ← Worker script name
```

---

## Custom Worker domains

Set `WorkerArgs.Domains` and provide a `ZoneID` in `AppConfig.Cloudflare`:

```go
App: &forge.AppConfig{
    Name: "my-app",
    Home: "cloudflare",
    Cloudflare: &forge.CloudflareConfig{
        AccountID: "abc123",
        ZoneID:    "xyz789",  // required for custom domains
    },
},
Run: func(ctx *forge.RunContext) error {
    cf.NewWorker(ctx, "Api", &cf.WorkerArgs{
        Handler: "worker/index.ts",
        Domains: []string{"api.example.com"},
    })
    return nil
},
```

---

## Go WASM Workers

forge can compile a Go package to WebAssembly at deploy time using `GOARCH=wasm GOOS=wasip1`:

```go
cf.NewWorker(ctx, "GoWorker", &cf.WorkerArgs{
    GoHandler: "./cmd/worker",  // path to a Go package
})
```

The Go toolchain must be in `PATH`. The compiled WASM binary is uploaded as a `WebassemblyBinding` with a thin JS wrapper using the `@cloudflare/workers-wasi` shim. See [constructs/cloudflare/worker.md](../constructs/cloudflare/worker.md) for details.

---

## Full example — aws+cloudflare

```go
package main

import (
    forge "github.com/sst-go/forge"
    "github.com/sst-go/forge/constructs"
    cf "github.com/sst-go/forge/constructs/cloudflare"
)

func main() {
    forge.Run(&forge.Config{
        App: &forge.AppConfig{
            Name: "my-app",
            Home: "aws+cloudflare",
            Cloudflare: &forge.CloudflareConfig{
                AccountID: "abc123",
            },
        },
        Run: func(ctx *forge.RunContext) error {
            // AWS backend
            table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
                PrimaryIndex: constructs.PrimaryIndex{PartitionKey: "id"},
            })
            api := constructs.NewApiGatewayV2(ctx, "Api", nil)
            api.Route("GET /orders", &constructs.RouteArgs{
                Handler: "functions/orders/main.handler",
                Link:    []forge.Linkable{table},
            })

            // Cloudflare edge
            kv := cf.NewKVNamespace(ctx, "Cache", nil)
            cf.NewWorker(ctx, "Frontend", &cf.WorkerArgs{
                Handler:    "worker/index.ts",
                KVBindings: []*cf.KVNamespace{kv},
                Link:       []forge.Linkable{api},  // SST_API_API_URL injected
            })

            ctx.Export("apiUrl", api.URL())
            return nil
        },
    })
}
```
