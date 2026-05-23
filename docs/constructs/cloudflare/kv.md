# KVNamespace

Creates a Cloudflare Workers KV namespace.

```go
import cf "github.com/nimbus-local/forge/constructs/cloudflare"

kv := cf.NewKVNamespace(ctx, "Sessions", nil)
```

---

## KVNamespaceArgs

`KVNamespaceArgs` has no fields — the namespace name and account ID are derived from the construct name and `AppConfig.Cloudflare`.

---

## Methods

```go
func (k *KVNamespace) ID() pulumi.IDOutput         // namespace UUID
func (k *KVNamespace) Title() pulumi.StringOutput  // namespace title (qualified name)
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_KV_<NAME>_ID` | KV namespace UUID |
| `SST_KV_<NAME>_NAME` | KV namespace title |

---

## Binding to a Worker

Pass the `*KVNamespace` in `WorkerArgs.KVBindings`. The binding variable name is the SCREAMING_SNAKE_CASE of the construct name:

```go
kv := cf.NewKVNamespace(ctx, "Sessions", nil)

worker := cf.NewWorker(ctx, "Api", &cf.WorkerArgs{
    Handler:    "worker/index.ts",
    KVBindings: []*cf.KVNamespace{kv},
})
```

Inside the worker the namespace is available as `env.SESSIONS`:

```ts
const value = await env.SESSIONS.get("user:123");
await env.SESSIONS.put("user:123", JSON.stringify(user), { expirationTtl: 3600 });
```

---

## Linking to an AWS Lambda

```go
lambda := constructs.NewFunction(ctx, "Processor", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{kv},
})
```

Inside the Lambda:

```go
kvID   := os.Getenv("SST_KV_SESSIONS_ID")
kvName := os.Getenv("SST_KV_SESSIONS_NAME")
```
