# forge

Go-native drop-in replacement for SST (Serverless Stack), which entered maintenance mode in 2025. Replaces the TypeScript config layer with native Go and adds a one-command migration path (`forge migrate`).

---

## Commands

```bash
# Build the CLI
go build ./cmd/forge

# Run all tests
go test ./...

# Run unit tests only (no integration)
go test ./... -short

# Run with integration tag (real AWS)
go test ./... -tags integration

# Run e2e tests against the built binary
go test ./test/e2e/... -tags e2e

# Test coverage
go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out

# Install CLI locally
go install ./cmd/forge
```

---

## Project Identity

| Property | Value |
|---|---|
| Module path | `github.com/sst-go/forge` |
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
github.com/sst-go/forge/
│
├── forge.go                      CORE LIBRARY — Run(), Config, AppConfig,
│                                RunContext, Linkable interface, Pulumi runner
│
├── go.mod                       Module definition
│
├── constructs/
│   ├── helpers.go               qualifiedName(), defaultTags(), envKey(), panicOnErr()
│   ├── function.go              NewFunction() — Lambda + IAM role + log group + env injection
│   ├── api.go                   NewApiGatewayV2() — HTTP API with route() helper
│   ├── table.go                 NewDynamoDB() — table + GSI support
│   └── bucket.go                NewBucket() — S3 + CORS + public access block
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
│   └── migrate.go               migrate subcommand
│
└── examples/
    └── sst.config.go            Reference example (todo API app)
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
    forge "github.com/sst-go/forge"              // always alias as forge
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
- The SQS-based dev tunnel architecture (do implement the missing stub binary)
- The SSM path convention (`/forge/<app>/<stage>/<name>`)
- Cobra CLI structure in `cmd/forge/`
- Lipgloss styles defined in `cmd/forge/main.go` — use them everywhere

---

## Planned Features

Work through in this order (each builds on the previous):

### 1. Bootstrap Command + State Bucket Auto-Creation

**Problem:** First deploy fails if the S3 state bucket doesn't exist.

**Implement `forge bootstrap`:**
```
forge bootstrap [--stage <stage>]
  Creates the Pulumi state S3 bucket if it doesn't exist.
  Bucket name: <app>-<stage>-forge-state
  Bucket config: versioning enabled, SSE-S3, public access blocked, lifecycle rule (expire old state after 90 days)
  Idempotent — safe to run multiple times.
  Auto-runs at the start of every `forge deploy` if the bucket doesn't exist.
```

File: `cmd/forge/bootstrap.go`
Helper: `internal/bootstrap/bootstrap.go` — uses AWS SDK v2 s3 + s3control

The deploy command in `cmd/forge/deploy.go` should call `ensureBootstrapped()` before calling `runConfig("deploy", stage)`. `ensureBootstrapped()` checks if the bucket exists (HeadBucket) and creates it if not.

### 2. Multi-Stage Support

**Current gap:** Stages work but have no per-stage configuration overrides.

Add to `forge.go`:
```go
type Config struct {
    App    *AppConfig
    Stages map[string]*StageConfig   // NEW
    Run    func(ctx *RunContext) error
}

type StageConfig struct {
    Removal     RemovalPolicy
    AWSProfile  string
    AWSRegion   string
    Protected   bool                   // forge remove requires --force
    Tags        map[string]string
}
```

RunContext additions:
```go
type RunContext struct {
    pulumiCtx   *pulumi.Context
    Stage       string
    App         *AppConfig
    DevMode     bool
    IsProtected bool
}

func (r *RunContext) IsProduction() bool { return r.Stage == "production" || r.Stage == "prod" }
func (r *RunContext) StageIn(stages ...string) bool { ... }
```

Also add `forge stages` command: lists all deployed stages with last-deployed timestamp, resource count, and protected status.

The `runPulumi()` function must read StageConfig and:
1. Override `AWS_PROFILE` and `AWS_DEFAULT_REGION` env vars if set
2. Set `ctx.IsProtected`
3. Merge StageConfig.Tags into `defaultTags()`
4. Block `forge remove` if `Protected: true` and `--force` not passed

### 3. Missing AWS Constructs

#### `constructs/cron.go` — EventBridge Scheduler
```go
type CronArgs struct {
    Schedule string   // "rate(5 minutes)" or "cron(0 12 * * ? *)"
    Job      *FunctionArgs
    Enabled  bool
}
func NewCron(ctx *forge.RunContext, name string, args *CronArgs) *Cron
```
Creates: EventBridge Scheduler → Lambda invoke permission + IAM role.

#### `constructs/queue.go` — SQS Queue
```go
type QueueArgs struct {
    Consumer          *FunctionArgs
    Fifo              bool
    VisibilityTimeout int   // default 30
    BatchSize         int   // default 10
    DeadLetterQueue   bool  // creates DLQ with 3 max receive count
}
func NewQueue(ctx *forge.RunContext, name string, args *QueueArgs) *Queue
// LinkEnv: SST_QUEUE_<NAME>_URL, SST_QUEUE_<NAME>_ARN
```

#### `constructs/topic.go` — SNS Topic
```go
type TopicArgs struct {
    Subscribers []*FunctionArgs
    FIFO        bool
}
func NewTopic(ctx *forge.RunContext, name string, args *TopicArgs) *Topic
// LinkEnv: SST_TOPIC_<NAME>_ARN
```

#### `constructs/secret.go` — Managed Secret Reference
```go
// Secret fetches an SSM SecureString at deploy time and injects it into linked Lambdas.
// LinkEnv: SST_SECRET_<NAME> = <resolved value>
type SecretArgs struct {
    Default string   // WARNING: stored in Pulumi state — non-sensitive defaults only
}
func NewSecret(ctx *forge.RunContext, name string, args *SecretArgs) *Secret
```

### 4. Tests

#### Unit tests — `migrate/converter_test.go`
```go
func TestConvertFunction(t *testing.T)
func TestConvertApiGatewayV2(t *testing.T)
func TestConvertDynamoDB(t *testing.T)
func TestConvertBucket(t *testing.T)
func TestConvertRemovalPolicy(t *testing.T)
func TestConvertAppConfig(t *testing.T)
func TestConvertLinks(t *testing.T)
func TestConvertExports(t *testing.T)
func TestRoundTrip(t *testing.T)
```

Use `testdata/` with `.ts` input files and `.go.golden` expected outputs.

#### Unit tests — `secrets/manager_test.go`
Mock the SSM client with an interface. Cover Set, Get, Remove, List, LoadAll, and not-found error.

#### Unit tests — `constructs/helpers_test.go`
Cover `qualifiedName`, `envKey` (camelCase/kebab → SCREAMING_SNAKE), and `defaultTags`.

#### Unit tests — `internal/bootstrap/bootstrap_test.go`
Mock the S3 client. Test bucket creation and idempotency.

#### Integration tests — `test/integration/`
Tag `//go:build integration`. Deploy a minimal stack to a real AWS account, verify outputs, tear down.

```go
func MustDeploy(t *testing.T, cfg *forge.Config, stage string) map[string]auto.OutputMap
func MustRemove(t *testing.T, cfg *forge.Config, stage string)
func TestStage(t *testing.T) string  // returns "test-" + random suffix
```

#### E2E CLI tests — `test/e2e/`
Test the actual `forge` binary (tag `//go:build e2e`):
```go
func TestForgeDeploy(t *testing.T)
func TestForgeDiff(t *testing.T)
func TestForgeMigrate(t *testing.T)
func TestForgeSecretSetGet(t *testing.T)
```

Testing rules:
- `t.Parallel()` in all unit tests
- Mock AWS SDK clients via interfaces — never make real AWS calls in unit tests
- Integration tests must clean up with `defer remove`
- Target 70%+ coverage on `migrate/`, `secrets/`, `internal/bootstrap/`

### 5. Cloudflare Support

Add `constructs/cloudflare/` with Worker, KV, D1, and R2 constructs. Add `go.mod` dependency on `github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare`.

AppConfig gains:
```go
type AppConfig struct {
    Name       string
    Home       string              // "aws" | "cloudflare" | "aws+cloudflare"
    Removal    RemovalPolicy
    Cloudflare *CloudflareConfig
}
type CloudflareConfig struct {
    AccountID string   // defaults to CLOUDFLARE_ACCOUNT_ID
    ZoneID    string   // defaults to CLOUDFLARE_ZONE_ID
}
```

When `Home` includes "cloudflare", `runPulumi()` installs the Cloudflare plugin. Check for `CLOUDFLARE_API_TOKEN` (preferred) or `CLOUDFLARE_API_KEY`+`CLOUDFLARE_EMAIL` and give a helpful error if missing.

Worker construct compiles Go to WASM (`GoHandler`) or bundles JS/TS via esbuild (`Handler`). KV, D1, and R2 inject namespace IDs as Lambda env bindings.

### 6. Project Templates (`forge create`)

```
forge create <project-name> [--template <template>]
```

Templates under `templates/`: `go-api`, `go-crud`, `go-worker`, `fullstack`. Each has a `template.yaml` with variable definitions.

Implementation in `cmd/forge/create.go`:
1. Embed all templates with `//go:embed templates/**`
2. Prompt for variables using `bufio.Scanner` (no external prompt lib)
3. Execute `.tmpl` files via `text/template`, strip `.tmpl` extension
4. Run `go mod tidy` in each module dir
5. Print next steps

### 7. Documentation

Every exported type, function, method, and constant needs a godoc comment. `docs/` directory with getting-started, migration guide, config reference, per-construct references, and concept guides (stages, linking, secrets, dev-tunnel, state).

### 8. Deploy Output Enhancement

Parse `UpResult` and format a clean summary table after deployment showing resource changes (created/updated/deleted) and stack outputs.

---

## Dependencies

```
github.com/aws/aws-sdk-go-v2 v1.26.0
github.com/aws/aws-sdk-go-v2/config v1.27.0
github.com/aws/aws-sdk-go-v2/service/lambda v1.54.0
github.com/aws/aws-sdk-go-v2/service/sqs v1.31.0
github.com/aws/aws-sdk-go-v2/service/ssm v1.49.0
github.com/charmbracelet/lipgloss v0.10.0
github.com/charmbracelet/log v0.4.0
github.com/pulumi/pulumi-aws/sdk/v6/go/aws v6.27.0
github.com/pulumi/pulumi/sdk/v3 v3.113.0
github.com/spf13/cobra v1.8.0
```

Pending:
- `github.com/aws/aws-sdk-go-v2/service/s3` — bootstrap feature
- `github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare` — Cloudflare feature
- Templates use stdlib `text/template` and `embed` only

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
| `new sst.aws.StaticSite("X", {...})` | `constructs.NewStaticSite(ctx, "X", &constructs.StaticSiteArgs{...})` |
| `new sst.aws.NextjsSite("X", {...})` | `constructs.NewNextjsSite(ctx, "X", &constructs.NextjsSiteArgs{...})` |
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
| `PULUMI_CONFIG_PASSPHRASE` | user | Pulumi auto | State file encryption |
| `AWS_PROFILE` | StageConfig / user | AWS SDK | Credential profile |
| `AWS_DEFAULT_REGION` | StageConfig / user | AWS SDK | Target region |
| `CLOUDFLARE_API_TOKEN` | user | Pulumi CF plugin | CF authentication |
| `CLOUDFLARE_ACCOUNT_ID` | user | CF constructs | CF account |
| `CLOUDFLARE_ZONE_ID` | user | CF constructs | CF zone |
