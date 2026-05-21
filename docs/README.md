# forge documentation

forge is a Go-native drop-in replacement for SST (Serverless Stack). It replaces the TypeScript config layer with native Go and provides a one-command migration path (`forge migrate`).

---

## Guides

| Guide | Description |
|---|---|
| [Getting Started](getting-started.md) | Install forge, create a project, and deploy |
| [Migration from SST](migration.md) | Automated `forge migrate` and manual conversion steps |
| [Config Reference](config-reference.md) | Full `Config`, `AppConfig`, `StageConfig`, and `RunContext` reference |

---

## Constructs — AWS

| Construct | Description |
|---|---|
| [Function](constructs/function.md) | Lambda function |
| [ApiGatewayV2](constructs/api-gateway.md) | HTTP API (API Gateway v2) |
| [DynamoDB](constructs/dynamodb.md) | DynamoDB table with optional GSIs |
| [Bucket](constructs/bucket.md) | S3 bucket |
| [Queue](constructs/queue.md) | SQS queue with optional consumer Lambda and DLQ |
| [Topic](constructs/topic.md) | SNS topic with optional Lambda subscribers |
| [Cron](constructs/cron.md) | EventBridge Scheduler schedule |
| [Secret](constructs/secret.md) | SSM SecureString injected into Lambdas at deploy time |
| [StaticSite](constructs/static-site.md) | S3 + CloudFront static website (Vite, Hugo, Astro, etc.) |
| [NextjsSite](constructs/nextjs-site.md) | Next.js app on S3 + CloudFront + Lambda (via open-next) |
| [Service](constructs/service.md) | ECS Fargate service with optional ALB |

---

## Constructs — Cloudflare

| Construct | Description |
|---|---|
| [Worker](constructs/cloudflare/worker.md) | Cloudflare Worker (JS/TS or Go WASM) |
| [KVNamespace](constructs/cloudflare/kv.md) | Cloudflare Workers KV namespace |
| [D1Database](constructs/cloudflare/d1.md) | Cloudflare D1 SQLite database |
| [R2Bucket](constructs/cloudflare/r2.md) | Cloudflare R2 object storage |

---

## Concepts

| Concept | Description |
|---|---|
| [Stages](concepts/stages.md) | Stage isolation, per-stage config, personal dev stages |
| [Linking](concepts/linking.md) | Passing resource identifiers between constructs |
| [Secrets](concepts/secrets.md) | SSM-backed secrets, CLI management, dev injection |
| [Dev Tunnel](concepts/dev-tunnel.md) | Running Lambda handlers locally with real AWS triggers |
| [State](concepts/state.md) | S3 Pulumi state backend, bootstrap, SST Ion migration |
| [Cloudflare](concepts/cloudflare.md) | Deploying Workers, KV, D1, and R2 alongside or instead of AWS |
| [GitHub Actions CI/CD](concepts/ci.md) | OIDC auth, multi-stage pipelines, PR previews, secret management |

---

## Quick links

- [GitHub](https://github.com/sst-go/forge)
- [SST v3 → forge mapping](migration.md#construct-mapping)
- [CLI reference](getting-started.md#cli-commands)
