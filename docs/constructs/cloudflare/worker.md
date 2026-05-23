# Worker

Creates a Cloudflare Workers script from a JS/TS entry point or a Go package compiled to WASM.

```go
import cf "github.com/nimbus-local/forge/constructs/cloudflare"

worker := cf.NewWorker(ctx, "Api", &cf.WorkerArgs{
    Handler: "worker/index.ts",
})
```

---

## WorkerArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Handler` | `string` | `""` | Path to JS/TS entry point. Bundled with `esbuild` CLI if in PATH, otherwise read as-is. |
| `GoHandler` | `string` | `""` | Path to a Go package. Compiled to WASM (`GOARCH=wasm GOOS=wasip1`) at deploy time. |
| `CompatibilityDate` | `string` | `""` | Workers runtime version pin (e.g. `"2024-01-01"`). |
| `KVBindings` | `[]*KVNamespace` | nil | KV namespaces bound to the Worker. |
| `D1Bindings` | `[]*D1Database` | nil | D1 databases bound to the Worker. |
| `R2Bindings` | `[]*R2Bucket` | nil | R2 buckets bound to the Worker. |
| `Link` | `[]forge.Linkable` | nil | Resources whose `SST_*` env vars are injected as plain-text bindings. |
| `Domains` | `[]string` | nil | Custom hostnames (requires `ZoneID` in `CloudflareConfig`). |

Exactly one of `Handler` or `GoHandler` must be set.

---

## Methods

```go
func (w *Worker) Name() pulumi.StringOutput  // deployed Worker script name
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_WORKER_<NAME>_NAME` | Cloudflare Worker script name |

---

## JS/TS Workers

forge uses the `esbuild` CLI to bundle the entry point if it is in `PATH`. Set `CompatibilityDate` to pin the Workers runtime:

```go
worker := cf.NewWorker(ctx, "Api", &cf.WorkerArgs{
    Handler:           "worker/index.ts",
    CompatibilityDate: "2024-09-23",
})
```

The worker script:

```ts
export default {
    async fetch(request: Request, env: Env): Promise<Response> {
        return new Response("Hello from Workers!");
    },
};
```

---

## Go WASM Workers

forge compiles Go packages to WASM at deploy time using `GOARCH=wasm GOOS=wasip1`. The WASM binary is attached as a `WebassemblyBinding` named `GOWORKER` with a thin JS wrapper that uses the `@cloudflare/workers-wasi` shim:

```go
worker := cf.NewWorker(ctx, "GoApi", &cf.WorkerArgs{
    GoHandler: "./cmd/worker",
})
```

Requirements:
- Go toolchain in `PATH`
- The package must implement the WASI fetch interface or `syscall/js` exports

---

## Bindings

KV, D1, and R2 resources are bound by their SCREAMING_SNAKE_CASE name. Linked forge resources are injected as plain-text bindings using `SST_*` keys.

```go
kv  := cf.NewKVNamespace(ctx, "Sessions", nil)
db  := cf.NewD1Database(ctx, "Users", nil)
api := constructs.NewApiGatewayV2(ctx, "Api", nil)

worker := cf.NewWorker(ctx, "Frontend", &cf.WorkerArgs{
    Handler:    "worker/index.ts",
    KVBindings: []*cf.KVNamespace{kv},
    D1Bindings: []*cf.D1Database{db},
    Link:       []forge.Linkable{api},
})
```

Inside the worker:

```ts
// KV: SESSIONS (binding name from envKey("Sessions"))
const session = await env.SESSIONS.get(key);

// D1: USERS
const result = await env.USERS.prepare("SELECT * FROM users").all();

// Linked API: plain-text binding
const apiURL = env.SST_API_API_URL;
```

---

## Custom Domains

Attach custom hostnames by setting `Domains` and `ZoneID` in `AppConfig.Cloudflare`:

```go
forge.Run(&forge.Config{
    App: &forge.AppConfig{
        Name: "myapp",
        Home: "cloudflare",
        Cloudflare: &forge.CloudflareConfig{
            AccountID: "abc123",
            ZoneID:    "xyz789",
        },
    },
    Run: func(ctx *forge.RunContext) error {
        cf.NewWorker(ctx, "Api", &cf.WorkerArgs{
            Handler: "worker/index.ts",
            Domains: []string{"api.example.com"},
        })
        return nil
    },
})
```
