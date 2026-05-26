# forge

Go-native drop-in replacement for SST (Serverless Stack), which entered maintenance mode in 2025. Replaces the TypeScript config layer with native Go and adds a one-command migration path (`forge migrate`).

> Build commands, PR checklist, and contributor setup are in [`.github/CONTRIBUTING.md`](.github/CONTRIBUTING.md).

---

## Project Identity

| Property | Value |
|---|---|
| Module path | `github.com/nimbus-local/forge` |
| Language | Go 1.22+ |
| IaC engine | Pulumi Automation API (inline programs) |
| State backend | S3 (Pulumi-compatible — importable from SST v3 Ion) |
| CLI binary | `forge` (`cmd/forge/`) |
| Config file | `infra/sst.config.go` (separate Go module, `package main`) |
| SST parity target | SST v3 Ion feature set |

---

## Architecture

### The Two-Module Pattern

forge uses a **two-module design** that is critical to understand:

```
my-project/
├── go.mod                 ← App module (your Lambda handlers, NO forge/Pulumi dep)
├── functions/
│   └── api/main.go        ← Lambda handler binaries compiled separately
└── infra/
    ├── go.mod             ← Infra module (imports forge + Pulumi)
    └── sst.config.go      ← Infrastructure definition (package main)
```

Lambda handler binaries must not carry Pulumi as a dependency — the two-module split enforces this. The CLI operates in the `infra/` directory.

### Execution Flow

```
forge deploy --stage prod
    │
    ▼
cmd/forge/runner.go: findConfig()
    │  Discovers infra/sst.config.go or sst.config.go
    │
    ▼
cmd/forge/runner.go: runConfig("deploy", "prod")
    │  Sets env vars: FORGE_MODE=deploy, FORGE_STAGE=prod
    │  Runs: go run . (inside infra/ directory)
    │
    ▼
User's infra/sst.config.go: main() → forge.Run(&forge.Config{...})
    │
    ▼
forge.go: Run() reads FORGE_MODE → calls runPulumi(cfg, "prod", "up")
    │
    ▼
forge.go: runPulumi()
    │  Creates Pulumi inline program wrapping user's Config.Run func
    │  auto.UpsertStackInlineSource(ctx, stackName, appName, pulumiProg)
    │  Installs AWS Pulumi plugin
    │  Calls stack.Up() → Pulumi deploys resources
    │
    ▼
User's Config.Run(ctx *RunContext)
    │  Calls construct constructors (NewFunction, NewApiGatewayV2, etc.)
    │  Each constructor creates Pulumi resources via pulumi-aws SDK
    │  ctx.Export() exposes stack outputs
    ▼
Done — resources deployed, outputs printed
```

**Key insight:** The CLI never touches Pulumi directly. It only sets env vars and runs `go run .`. All Pulumi logic lives in the forge library imported by the user's config.

### The Linkable Contract

Every construct implements the `Linkable` interface:

```go
// In forge.go — only constructs provided by this module are intended to implement this.
type Linkable interface {
    LinkEnv() pulumi.StringMap   // env vars to inject into linked Functions
    LinkName() string            // construct name for debugging
}
```

When a Function is created with `Link: []forge.Linkable{table, bucket}`, it merges the `LinkEnv()` output of each linked resource into its Lambda environment.

**Env var key convention** (matches SST exactly so handler code is portable):
```
SST_<RESOURCE_TYPE>_<SCREAMING_SNAKE_NAME>_<ATTRIBUTE>

SST_TABLE_MY_TABLE_NAME        ← DynamoDB table name
SST_TABLE_MY_TABLE_ARN         ← DynamoDB table ARN
SST_BUCKET_UPLOADS_NAME        ← S3 bucket name
SST_BUCKET_UPLOADS_ARN         ← S3 bucket ARN
SST_API_MY_API_URL             ← API Gateway URL
SST_FUNCTION_MY_FN_ARN         ← Lambda function ARN
SST_WORKER_MY_WORKER_URL       ← Cloudflare Worker URL (to be added)
```

### Resource Naming

All physical AWS resource names are stage-qualified to prevent collisions:

```go
func qualifiedName(ctx *forge.RunContext, name string) string {
    return fmt.Sprintf("%s-%s-%s", ctx.App.Name, ctx.Stage, name)
}
// "my-app" + "prod" + "UsersTable" → "my-app-prod-UsersTable"
```

Tags applied to every resource:
```
forge:app    = <appName>
forge:stage  = <stage>
forge:name   = <construct logical name>
```

### State Backend

Pulumi state is stored in S3:
```
s3://<app>-<stage>-forge-state
```
Override with `FORGE_STATE_BUCKET`. The bucket must exist before first deploy — `forge bootstrap` creates it. SST v3 Ion users can point `FORGE_STATE_BUCKET` at their existing Ion state bucket (same Pulumi S3 backend format).

### Secret Management

Secrets are stored in SSM Parameter Store as `SecureString`:
```
/forge/<appName>/<stage>/<secretName>
```

In dev mode, all secrets are loaded via `secrets.Manager.LoadAll()` and injected into the local process environment before running handlers.

### Dev Tunnel Architecture

```
                    AWS
┌──────────────────────────────────────────────────────┐
│  Real trigger   →   Stub Lambda (go binary)          │
│  (API GW, SQS,      ├─ Receives invocation           │
│   EventBridge)      ├─ Publishes to SQS request Q    │
│                     └─ Long-polls SQS response Q      │
│                                                       │
│  SQS request queue:  forge-<app>-<stage>-req          │
│  SQS response queue: forge-<app>-<stage>-res          │
└───────────────────────────────┬──────────────────────┘
                                │ SQS
                    Local machine (forge dev)
┌───────────────────────────────▼──────────────────────┐
│  dev/tunnel.go: Tunnel.Poll()                        │
│  ├─ Receives event from SQS request queue            │
│  ├─ Looks up registered handler binary               │
│  ├─ Runs binary with event piped to stdin            │
│  ├─ Reads response from stdout                       │
│  └─ Sends response to SQS response queue             │
└──────────────────────────────────────────────────────┘
```

**Stub Lambda binary** (`cmd/forge-stub/main.go` — TO BE CREATED):
- A single Go binary deployed as all stub Lambdas
- Reads `FORGE_REQUEST_QUEUE_URL` and `FORGE_RESPONSE_QUEUE_URL` from env
- Reads `FORGE_FUNCTION_ID` to identify itself in responses
- On invocation: publishes `{id, functionArn, event, context}` to request queue
- Polls response queue with matching `id` for up to 29s (Lambda timeout - 1s)

### Migration Tool

`migrate/converter.go` does regex/heuristic parsing of `sst.config.ts`. Two-phase approach:
1. **Structural extraction** — pulls out `app()` config block and `run()` body using regex
2. **Line-by-line conversion** — transforms `new sst.aws.X(...)` calls to Go constructors

Known limitations:
- Multi-line constructor args (spanning 3+ lines) emit TODO comments
- Ternary expressions in args are not fully converted
- `$output()` / `$interpolate()` calls need manual conversion
- `sst.Secret` references need manual wiring

---

## File Structure

```
github.com/nimbus-local/forge/
│
├── forge.go                      CORE LIBRARY — Run(), Config, AppConfig,
│                                RunContext, Linkable interface, Pulumi runner
│
├── go.mod                       Module definition
│
├── constructs/
│   ├── helpers.go               qualifiedName(), defaultTags(), envKey(), panicOnErr(), resolveLogRetention()
│   ├── function.go              NewFunction() — Lambda + IAM role + log group + env injection + optional Code zip
│   ├── api.go                   NewApiGatewayV2() — HTTP API with route() helper
│   ├── table.go                 NewDynamoDB() — table + GSI support
│   ├── bucket.go                NewBucket() — S3 + CORS + public access block + SSE-KMS + lifecycle
│   ├── kms.go                   NewKMSKey() — KMS symmetric key + alias + kmsGrant() helper
│   └── service.go               NewService() — ECS Fargate + ALB
│
├── secrets/
│   └── manager.go               SSM Parameter Store CRUD (Set/Get/Remove/List/LoadAll)
│
├── dev/
│   └── tunnel.go                SQS-based dev tunnel (poll + local handler execution)
│
├── migrate/
│   └── converter.go             sst.config.ts → sst.config.go converter
│
├── cmd/forge/
│   ├── main.go                  Cobra root command, global flags, lipgloss styles
│   ├── runner.go                findConfig() + runConfig() — the core CLI dispatch
│   ├── deploy.go                deploy / remove / diff subcommands
│   ├── dev.go                   dev subcommand
│   ├── secret.go                secret set/get/remove/list subcommands
│   ├── migrate.go               migrate subcommand
│   ├── console.go               console subcommand — local HTTP server + data API
│   └── consoleassets/
│       └── index.html           embedded single-page console UI
│
└── examples/
    ├── sst.config.go            Reference example (todo API app)
    ├── checklist-simple/        Next.js + DynamoDB — anonymous cookie-keyed lists
    │   ├── web/                 Next.js 14 App Router (TypeScript)
    │   └── infra/               forge infra config
    └── checklist-full/          Next.js + Go Lambda + DynamoDB + GitHub OAuth
        ├── functions/api/       Go Lambda handler (CRUD)
        ├── web/                 Next.js 14 + Auth.js (GitHub OAuth)
        ├── infra/               forge infra config
        └── Makefile             build + deploy helpers
```

---

## Code Conventions

### Naming
- Construct constructors: `NewXxx(ctx *forge.RunContext, name string, args *XxxArgs) *Xxx`
- Args structs: `XxxArgs` (nil-safe — all constructors handle `args == nil`)
- Physical resource names: always use `qualifiedName(ctx, name)` helper
- Tags: always call `defaultTags(ctx, name)` — every resource gets forge tags

### Error handling in constructs
Constructs use `panicOnErr()` (not returning errors). This is intentional — Pulumi inline programs propagate panics correctly and they display cleanly in deploy output. CLI commands use standard `error` returns.

### Linkable implementation
Every construct that can be linked must implement both unexported methods:
```go
func (x *MyConstruct) LinkEnv() pulumi.StringMap  { ... }
func (x *MyConstruct) LinkName() string           { return x.name }
```

### Imports
```go
import (
    forge "github.com/nimbus-local/forge"              // always alias as forge
    "github.com/pulumi/pulumi-aws/sdk/v6/go/aws/lambda"
    "github.com/pulumi/pulumi/sdk/v3/go/pulumi"
)
```

### Style
- Box-drawing comment separators: `// ── Section ─────────────────` (not `//---`)
- Exported types and functions: full godoc comments
- Unexported helpers: short inline comments
- No `log.Fatal` in library code — use `panicOnErr()` or return errors
- CLI output: use lipgloss styles from `cmd/forge/main.go` (bold, green, red, dim)

---

## Do Not Change

These are working and correct — do not refactor unless a feature explicitly requires it:

- The two-module pattern (`infra/` separation)
- The `FORGE_MODE` / `FORGE_STAGE` env var dispatch mechanism in `forge.go`
- The `Linkable` interface (`LinkEnv`/`LinkName` are exported; only forge constructs should implement it)
- The `qualifiedName()` and `envKey()` naming helpers
- The SQS-based dev tunnel architecture
- The SSM path convention (`/forge/<app>/<stage>/<name>`)
- Cobra CLI structure in `cmd/forge/`
- Lipgloss styles defined in `cmd/forge/main.go` — use them everywhere

### NextjsSite: Lambda Function URL auth

`NewNextjsSite` uses `AuthorizationType: NONE` on the Lambda Function URL with two public
resource-based policy statements. **Do not switch to `AWS_IAM`.** The `AWS_IAM` approach
requires CloudFront OAC with Lambda signing, which does not work reliably and results in
403 errors from CloudFront.

The resource-based policy must grant **both** `lambda:InvokeFunctionUrl` and
`lambda:InvokeFunction` to `Principal: "*"`. Granting only one action causes AWS to show
a console warning and requests will be denied.

`NONE` does not mean the application is unauthenticated. It means AWS does not require
SigV4 signing to reach the Lambda. Application-level auth (Auth.js, GitHub OAuth, JWT
middleware) still runs inside the Lambda and is unaffected. A Next.js site using GitHub
OAuth works correctly with `AuthorizationType: NONE`.

### NextjsSite: next-auth integration gotchas

These were discovered building `examples/checklist-full`. Violating any of them produces
silent or misleading errors (server error pages, NO_SECRET, assets 404ing).

**1. `SessionProvider` must be in a `"use client"` wrapper.**

`SessionProvider` from `next-auth/react` uses React Context. Rendering it directly in a
Server Component (e.g. `app/layout.tsx`) throws `Error: React Context is unavailable in
Server Components`. Always extract it:

```tsx
// app/providers.tsx
'use client'
import { SessionProvider } from 'next-auth/react'
export function Providers({ children, session }) {
  return <SessionProvider session={session}>{children}</SessionProvider>
}

// app/layout.tsx  ← Server Component, no 'use client'
import { Providers } from './providers'
export default async function RootLayout({ children }) {
  const session = await getServerSession(authOptions)
  return <html><body><Providers session={session}>{children}</Providers></body></html>
}
```

**2. Never use the bare next-auth middleware re-export.**

```typescript
// ❌ reads NEXTAUTH_SECRET directly, ignores authOptions, breaks behind CloudFront
export { default } from 'next-auth/middleware'
```

The bare re-export reads `process.env.NEXTAUTH_SECRET` at the module level and derives the
redirect URL from the request `Host` header. Behind CloudFront + Lambda Function URL the
`Host` header is the Lambda URL, not the CloudFront domain. This causes:
- `[next-auth][error][NO_SECRET]` if only `SST_SECRET_NEXTAUTH_SECRET` is set
- Redirects that send the browser to the raw Lambda URL domain
- Static asset 404s because `/_next/static/*` only exists on CloudFront/S3

Always use a custom middleware with `getToken`. `NewNextjsSite` sets `x-forwarded-host`
automatically via a CloudFront viewer-request function, so the correct public domain is
always available in the request headers:

```typescript
// ✅ middleware.ts
import { getToken } from 'next-auth/jwt'
import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'

export async function middleware(req: NextRequest) {
  const token = await getToken({
    req,
    secret: process.env.SST_SECRET_NEXTAUTH_SECRET ?? process.env.NEXTAUTH_SECRET,
  })
  if (!token) {
    const host = req.headers.get('x-forwarded-host') ?? req.nextUrl.host
    const loginUrl = new URL('/login', `https://${host}`)
    loginUrl.searchParams.set('callbackUrl', req.nextUrl.href)
    return NextResponse.redirect(loginUrl)
  }
  return NextResponse.next()
}
```

**3. `authOptions` must declare `pages.signIn`.**

Without it, next-auth redirects unauthenticated users to its built-in `/api/auth/signin`
page rather than your custom login page. Add to `authOptions`:

```typescript
pages: {
  signIn: '/login',
},
```

**4. GitHub OAuth app callback URL.**

The GitHub OAuth app's "Authorization callback URL" must be:
```
<url output from forge deploy>/api/auth/callback/github
```
e.g. `https://d6ee090je5y94.cloudfront.net/api/auth/callback/github`. If this does not
match exactly (including scheme and path), GitHub will reject the OAuth redirect and the
sign-in flow will fail with a redirect_uri_mismatch error.

---

## Planned Features

### Configurable Resource Name Suffix

Currently all S3 bucket names (state bucket and construct buckets) are suffixed with the
AWS account ID to guarantee global uniqueness. A future option to override this suffix
would be useful when:
- Migrating from SST or another tool that used a different naming convention
- Teams that prefer a shorter custom suffix (e.g. a project code) over the 12-digit account ID
- CI environments that need deterministic, human-readable bucket names

Add `BucketSuffix` to `AppConfig`:
```go
type AppConfig struct {
    Name         string
    Home         string
    Removal      RemovalPolicy
    BucketSuffix string   // overrides the account ID suffix on all S3 bucket names
    Cloudflare   *CloudflareConfig
}
```

When `BucketSuffix` is set, use it instead of the account ID in both `bucketName()` (constructs)
and `BucketName()` (state bucket). When empty, fall back to the account ID (current behaviour).
Store the suffix on `RunContext` alongside `AccountID` so constructs can access it.

### Missing AWS Constructs

Constructs are being added in this order (dependencies first):

1. **`constructs/vpc.go`** — `NewVpc` — VPC with public/private/isolated subnets, NAT, VPC endpoints. No LinkEnv; consumed by Database, Cache, Efs, Service.
2. **`constructs/email.go`** — `NewEmail` — SES domain or address identity + DKIM + optional configuration set. `SST_EMAIL_<NAME>_SENDER`, `SST_EMAIL_<NAME>_CONFIG_SET`. IAM: `ses:SendEmail`, `ses:SendRawEmail`.
3. **`constructs/cognito_user_pool.go`** — `NewCognitoUserPool` — User Pool + default app client + optional Lambda triggers. `SST_COGNITO_<NAME>_USER_POOL_ID`, `SST_COGNITO_<NAME>_CLIENT_ID`.
4. **`constructs/cognito_identity_pool.go`** — `NewCognitoIdentityPool` — Federated identity pool linking user pools to IAM roles. `SST_IDENTITY_POOL_<NAME>_ID`.
5. **`constructs/kinesis_stream.go`** — `NewKinesisStream` — Kinesis Data Stream with optional Lambda consumers. `SST_KINESIS_<NAME>_STREAM_NAME`, `SST_KINESIS_<NAME>_STREAM_ARN`.
6. **`constructs/bus.go`** — `NewBus` — EventBridge custom event bus with rules and Lambda/Queue targets. `SST_BUS_<NAME>_NAME`, `SST_BUS_<NAME>_ARN`. IAM: `events:PutEvents`.
7. **`constructs/apigateway_websocket.go`** — `NewApiGatewayWebSocket` — WebSocket API with route handlers. `SST_WS_<NAME>_URL`, `SST_WS_<NAME>_MGMT_URL`. IAM: `execute-api:ManageConnections`.
8. **`constructs/step_functions.go`** — `NewStepFunctions` — State machine (Standard or Express) from raw ASL JSON. `SST_SFN_<NAME>_ARN`, `SST_SFN_<NAME>_NAME`. IAM: `states:StartExecution`, `states:DescribeExecution`, `states:StopExecution`.
9. **`constructs/database.go`** — `NewDatabase` — Aurora Serverless v2 or RDS Postgres/MySQL. `SST_DATABASE_<NAME>_HOST/PORT/NAME/USERNAME/SECRET_ARN/CLUSTER_ARN`. IAM: `secretsmanager:GetSecretValue`; Aurora Data API: `rds-data:*`.
10. **`constructs/cache.go`** — `NewCache` — ElastiCache Serverless or replication group (Redis/Valkey). `SST_CACHE_<NAME>_HOST/PORT/TLS/AUTH_TOKEN`.
11. **`constructs/efs.go`** — `NewEfs` — EFS filesystem + access point wired into Lambda mount. `SST_EFS_<NAME>_ACCESS_POINT_ARN`, `SST_EFS_<NAME>_MOUNT_PATH`. IAM: `elasticfilesystem:Client*`.
12. **`constructs/appsync.go`** — `NewAppSync` — AppSync GraphQL API with schema, data sources, resolvers. `SST_APPSYNC_<NAME>_URL`, `SST_APPSYNC_<NAME>_API_ID`. IAM: `appsync:GraphQL`.
13. **`constructs/opensearch.go`** — `NewOpenSearch` — OpenSearch Service domain. `SST_OPENSEARCH_<NAME>_ENDPOINT`, `SST_OPENSEARCH_<NAME>_ARN`. IAM: `es:ESHttp*`.
14. **`constructs/apigateway_v1.go`** — `NewApiGatewayV1` — REST API Gateway (v1). `SST_APIV1_<NAME>_URL`.
15. **`constructs/realtime.go`** — `NewRealtime` — IoT Core WebSocket endpoint for real-time pub/sub. `SST_REALTIME_<NAME>_ENDPOINT`, `SST_REALTIME_<NAME>_AUTHORIZER_ARN`.
16. **`constructs/dsql.go`** — `NewDsql` — Aurora DSQL serverless PostgreSQL cluster (requires Pulumi AWS v7.30+ support). `SST_DSQL_<NAME>_ENDPOINT`, `SST_DSQL_<NAME>_CLUSTER_ARN`.

#### `constructs/email.go` — SES Email Identity
```go
type EmailArgs struct {
    // Sender is either a full domain ("acme.com") or a single address ("no-reply@acme.com").
    // Domain identity: forge creates DKIM + MAIL FROM records and exports them as stack
    //   outputs — the operator must add them to DNS before SES will send.
    // Address identity: SES sends a one-click verification email; no DNS work required
    //   but sending is limited to that single address.
    Sender string
    // ReplyTo is injected into every send as the Reply-To header (optional).
    ReplyTo string
    // ConfigSet enables a SES Configuration Set for open/click/bounce tracking
    // via EventBridge or SNS. Requires a separate NewTopic or NewQueue to consume events.
    ConfigSet bool
}
func NewEmail(ctx *forge.RunContext, name string, args *EmailArgs) *Email
// LinkEnv: SST_EMAIL_<NAME>_SENDER   — verified From address
//          SST_EMAIL_<NAME>_CONFIG_SET — configuration set name (if ConfigSet: true)
```
Creates: SES domain or address identity + optional DKIM signing + optional configuration
set. When linked to a Function, automatically grants `ses:SendEmail` and
`ses:SendRawEmail` on the verified identity ARN — the Lambda can send immediately
without any manual IAM wiring.

DNS records for domain identities are exported as stack outputs so the operator knows
exactly what to add (DKIM CNAME × 3, MAIL FROM MX + TXT). SES stays in sandbox mode
until the account is manually moved to production via the AWS console — document this
prominently.

**Magic link pattern** (application code, not a construct):
A magic link flow needs `NewEmail` for sending plus application code in a Lambda:
1. Generate a signed, time-limited token (HMAC-SHA256 + expiry, or a UUID stored in
   DynamoDB with a TTL attribute).
2. Send the link via SES using the injected `SST_EMAIL_<NAME>_SENDER`.
3. On click, a second route validates the token (check DynamoDB TTL or verify HMAC),
   issues a session cookie, and deletes the token.
`NewDynamoDB` + `NewEmail` + `NewFunction` + `NewApiGatewayV2` is the complete
infrastructure for a magic link auth system. No additional construct needed.

### Smoke test infrastructure notes

Local smoke tests run against [Nimbus](https://github.com/nimbus-local/nimbus) (`make smoke`).
Not all AWS services are emulated. Constructs blocked on Nimbus support are unit-tested only
until the service is added.

| Construct | Nimbus support | Smoke status |
|---|---|---|
| `NewVpc` | ✗ EC2 not emulated | unit tests only — awaiting Nimbus EC2 support |
| `NewEmail` | ✗ SESv2 not emulated (SES v1 only) | unit tests only — awaiting Nimbus SESv2 support |
| `NewCognitoUserPool` / `NewCognitoIdentityPool` | ✓ `cognito` | planned |
| `NewDatabase` | ✓ `rds` | planned |
| `NewCache` | ✓ `elasticache` | planned |
| `NewBus` | ✓ `eventbridge` | planned |
| `NewKinesisStream` | ✓ `kinesis` | smoke added |

### Remaining roadmap items

- ✅ `forge dev` tunnel — implementation complete; integration test against Nimbus in progress
- ✅ `NewVpc` — implemented; unit tests pass; smoke blocked (Nimbus EC2 not supported)
- ✅ `NewEmail` — implemented; unit tests pass; smoke blocked (Nimbus SESv2 not supported)
- ✅ `NewCognitoUserPool` — implemented; unit tests pass; smoke planned
- ✅ `NewCognitoIdentityPool` — implemented; unit tests pass; smoke planned
- ✅ `NewKinesisStream` — implemented; unit tests pass; smoke added
- `NewBus` (EventBridge) — planned
- `NewApiGatewayWebSocket` — planned
- `NewStepFunctions` — planned
- `NewDatabase` (Aurora/RDS) — planned
- `NewCache` (ElastiCache) — planned
- `NewEfs` — planned
- `NewAppSync` — planned
- `NewOpenSearch` — planned
- `NewApiGatewayV1` — planned
- `NewRealtime` (IoT) — planned
- `NewDsql` — planned (pending Pulumi provider support)
- Drift detection — `forge drift` compares live AWS state against Pulumi state

---

## Dependencies

Actual versions from go.mod (use `go get module@latest` to update):

```
github.com/aws/aws-sdk-go-v2 v1.41.7
github.com/aws/aws-sdk-go-v2/config v1.32.18
github.com/aws/aws-sdk-go-v2/service/s3 v1.101.0
github.com/aws/aws-sdk-go-v2/service/sqs v1.42.27
github.com/aws/aws-sdk-go-v2/service/ssm v1.68.6
github.com/aws/aws-sdk-go-v2/service/sts v1.42.1
github.com/charmbracelet/lipgloss v1.1.0
github.com/pulumi/pulumi-aws/sdk/v7 v7.30.0   ← NOTE: v7, not v6
github.com/pulumi/pulumi-cloudflare/sdk/v5 v5.49.1
github.com/pulumi/pulumi/sdk/v3 v3.243.0
github.com/spf13/cobra v1.10.2
```

All new constructs must import `github.com/pulumi/pulumi-aws/sdk/v7/go/aws/...`.

---

## SST v3 → forge Mapping

| SST TypeScript | forge Go |
|---|---|
| `new sst.aws.Function("X", {...})` | `constructs.NewFunction(ctx, "X", &constructs.FunctionArgs{...})` |
| `new sst.aws.ApiGatewayV2("X")` | `constructs.NewApiGatewayV2(ctx, "X", nil)` |
| `api.route("GET /", fn)` | `api.Route("GET /", &constructs.RouteArgs{Function: fn})` |
| `new sst.aws.DynamoDB("X", {...})` | `constructs.NewDynamoDB(ctx, "X", &constructs.DynamoDBArgs{...})` |
| `new sst.aws.Bucket("X")` | `constructs.NewBucket(ctx, "X", nil)` |
| `new sst.aws.Cron("X", {...})` | `constructs.NewCron(ctx, "X", &constructs.CronArgs{...})` |
| `new sst.aws.Queue("X", {...})` | `constructs.NewQueue(ctx, "X", &constructs.QueueArgs{...})` |
| `new sst.aws.Topic("X", {...})` | `constructs.NewTopic(ctx, "X", &constructs.TopicArgs{...})` |
| `new sst.Secret("X")` | `constructs.NewSecret(ctx, "X", nil)` |
| `new aws.kms.Key("X", {...})` | `constructs.NewKMSKey(ctx, "X", &constructs.KMSKeyArgs{...})` |
| `new sst.aws.StaticSite("X", {...})` | `constructs.NewStaticSite(ctx, "X", &constructs.StaticSiteArgs{...})` |
| `new sst.aws.NextjsSite("X", {...})` | `constructs.NewNextjsSite(ctx, "X", &constructs.NextjsSiteArgs{...})` |
| `new sst.aws.Service("X", {...})` | `constructs.NewService(ctx, "X", &constructs.ServiceArgs{...})` |
| `new sst.aws.Cognito("X", {...})` | `constructs.NewCognitoUserPool(ctx, "X", &constructs.CognitoUserPoolArgs{...})` |
| `new sst.aws.CognitoIdentityPool("X", {...})` | `constructs.NewCognitoIdentityPool(ctx, "X", &constructs.CognitoIdentityPoolArgs{...})` |
| `new sst.aws.Email("X", {...})` | `constructs.NewEmail(ctx, "X", &constructs.EmailArgs{...})` |
| `new sst.aws.KinesisStream("X", {...})` | `constructs.NewKinesisStream(ctx, "X", &constructs.KinesisStreamArgs{...})` |
| `new sst.aws.Bus("X", {...})` | `constructs.NewBus(ctx, "X", &constructs.BusArgs{...})` |
| `new sst.aws.ApiGatewayWebSocket("X", {...})` | `constructs.NewApiGatewayWebSocket(ctx, "X", &constructs.ApiGatewayWebSocketArgs{...})` |
| `new sst.aws.StepFunctions("X", {...})` | `constructs.NewStepFunctions(ctx, "X", &constructs.StepFunctionsArgs{...})` |
| `new sst.aws.Postgres("X", {...})` | `constructs.NewDatabase(ctx, "X", &constructs.DatabaseArgs{Engine: "aurora-postgresql"})` |
| `new sst.aws.Redis("X", {...})` | `constructs.NewCache(ctx, "X", &constructs.CacheArgs{...})` |
| `new sst.aws.Efs("X", {...})` | `constructs.NewEfs(ctx, "X", &constructs.EfsArgs{...})` |
| `new sst.aws.Vpc("X", {...})` | `constructs.NewVpc(ctx, "X", &constructs.VpcArgs{...})` |
| `new sst.aws.AppSync("X", {...})` | `constructs.NewAppSync(ctx, "X", &constructs.AppSyncArgs{...})` |
| `new sst.aws.OpenSearch("X", {...})` | `constructs.NewOpenSearch(ctx, "X", &constructs.OpenSearchArgs{...})` |
| `new sst.aws.ApiGatewayV1("X", {...})` | `constructs.NewApiGatewayV1(ctx, "X", &constructs.ApiGatewayV1Args{...})` |
| `new sst.aws.Realtime("X", {...})` | `constructs.NewRealtime(ctx, "X", &constructs.RealtimeArgs{...})` |
| `new sst.cloudflare.Worker("X", {...})` | `cf.NewWorker(ctx, "X", &cf.WorkerArgs{...})` |
| `new sst.cloudflare.KV("X")` | `cf.NewKVNamespace(ctx, "X", nil)` |
| `new sst.cloudflare.D1("X")` | `cf.NewD1Database(ctx, "X", nil)` |
| `new sst.cloudflare.Bucket("X")` | `cf.NewR2Bucket(ctx, "X", nil)` |
| `$config({app, run})` | `forge.Run(&forge.Config{App, Run})` |
| `input?.stage === "production"` | `ctx.IsProduction()` |
| `sst deploy` | `forge deploy` |
| `sst dev` | `forge dev` |
| `sst remove` | `forge remove` |
| `sst secret set X val` | `forge secret set X val` |
| `sst diff` | `forge diff` |

---

## Key Env Vars

| Variable | Set by | Read by | Purpose |
|---|---|---|---|
| `FORGE_MODE` | CLI runner | forge.Run() | deploy/remove/diff/dev |
| `FORGE_STAGE` | CLI runner / user | forge.Run(), constructs | Active stage name |
| `FORGE_APP` | user (optional) | secret.go CLI | App name for secret paths |
| `FORGE_STATE_BUCKET` | user (optional) | forge.go | Override S3 state bucket |
| `FORGE_FORCE_REMOVE` | CLI --force flag | forge.Run() | Bypass protected stage check |
| `FORGE_AWS_ENDPOINT` | user (optional) | all AWS SDK clients, Pulumi provider | Redirect all AWS API calls to a local emulator (e.g. Nimbus at `http://localhost:4566`) |
| `PULUMI_CONFIG_PASSPHRASE` | user | Pulumi auto | State file encryption |
| `AWS_PROFILE` | StageConfig / user | AWS SDK | Credential profile |
| `AWS_DEFAULT_REGION` | StageConfig / user | AWS SDK | Target region |
| `CLOUDFLARE_API_TOKEN` | user | Pulumi CF plugin | CF authentication |
| `CLOUDFLARE_ACCOUNT_ID` | user | CF constructs | CF account |
| `CLOUDFLARE_ZONE_ID` | user | CF constructs | CF zone |
