# forge — Go SST

A drop-in replacement for [SST](https://sst.dev) written in Go.
Powered by [Pulumi](https://pulumi.com) under the hood — **state files are fully compatible with SST v3 Ion**.

> SST entered maintenance mode in 2025 after the team shifted focus to OpenCode.
> forge picks up where SST left off, with a native Go config and zero Node.js dependency.

## Features

| Feature | SST v3 | forge |
|---|---|---|
| Config language | TypeScript | **Go** |
| IaC engine | Pulumi (Ion) | Pulumi (compatible state) |
| `deploy / remove / diff` | ✓ | ✓ |
| Live Lambda dev tunnel | ✓ | ✓ (SQS relay) |
| Secrets (SSM) | ✓ | ✓ |
| Multi-stage config | ✓ | ✓ |
| Per-stage AWS profile/region | ✓ | ✓ |
| Protected stages | ✓ | ✓ |
| Project scaffolding | — | ✓ `forge create` |
| Migration tool | — | ✓ `forge migrate` |
| Cloudflare Workers / KV / D1 / R2 | ✓ | ✓ |
| Node.js required | ✓ | ✗ |

---

## Installation

```bash
go install github.com/sst-go/forge/cmd/forge@latest
```

---

## Quick start

```bash
# Scaffold a new project
forge create my-api --template go-api
cd my-api

# Deploy to AWS
forge deploy
```

Available templates: `go-api`, `go-crud`, `go-worker`, `fullstack`

---

## Migrating from SST

### 1. One-command migration

```bash
# Inside your existing SST project:
forge migrate                        # converts ./sst.config.ts → ./infra/sst.config.go
forge migrate path/to/sst.config.ts  # explicit path
```

### 2. Review the output

The migrator handles the most common patterns automatically and emits
`// TODO:` comments for anything that needs manual attention.

```bash
cat infra/sst.config.go   # review
cd infra && go mod tidy   # fetch dependencies
```

### 3. Deploy

```bash
forge diff --stage dev      # preview changes
forge deploy --stage dev
forge deploy --stage production
```

### State file compatibility

If you were already on **SST v3 Ion**, your Pulumi state is in S3 and is fully
compatible. Set `FORGE_STATE_BUCKET` to point at the same bucket:

```bash
export FORGE_STATE_BUCKET=my-app-dev-forge-state
forge diff   # should show zero changes if the config was migrated correctly
```

---

## Writing a config from scratch

Create `infra/sst.config.go`:

```go
package main

import (
    forge "github.com/sst-go/forge"
    "github.com/sst-go/forge/constructs"
)

func main() {
    forge.Run(&forge.Config{
        App: &forge.AppConfig{
            Name: "my-app",
            Home: "aws",
        },
        // Optional: per-stage overrides
        Stages: map[string]*forge.StageConfig{
            "production": {
                Protected:  true,
                AWSProfile: "prod",
            },
        },
        Run: func(ctx *forge.RunContext) error {
            table := constructs.NewDynamoDB(ctx, "UsersTable", &constructs.DynamoDBArgs{
                PrimaryIndex: constructs.PrimaryIndex{PartitionKey: "id"},
            })

            fn := constructs.NewFunction(ctx, "Api", &constructs.FunctionArgs{
                Handler: "bootstrap",
                Link:    []forge.Linkable{table},
            })

            api := constructs.NewApiGatewayV2(ctx, "Api", nil)
            api.Route("GET /users", &constructs.RouteArgs{Function: fn})

            ctx.Export("url", api.URL())
            return nil
        },
    })
}
```

---

## Commands

```
forge create <name> [-t <template>]   Scaffold a new project
forge deploy [--stage <stage>]        Deploy your stack
forge dev    [--stage <stage>]        Live Lambda dev tunnel
forge diff   [--stage <stage>]        Preview changes (no deploy)
forge remove [--stage <stage>]        Destroy a stage
forge stages                          List deployed stages
forge secret set   <NAME> <VAL>       Store a secret in SSM
forge secret get   <NAME>             Retrieve a secret
forge secret remove <NAME>            Delete a secret
forge secret list                     List all secrets for stage
forge migrate [sst.config.ts]         Convert TS config to Go
forge bootstrap [--stage <stage>]     Create the Pulumi state S3 bucket
```

### Global flags

```
--stage, -s   Deployment stage (default: $USER or "dev")
--profile     AWS credentials profile
--region      AWS region
--config      Path to sst.config.go (default: ./infra/sst.config.go)
```

---

## Constructs

### AWS

All constructs implement `forge.Linkable` — link them to a Function to inject
their identifiers as environment variables.

| Construct | Usage | Env vars injected |
|---|---|---|
| `NewFunction` | Lambda function | `SST_FUNCTION_<NAME>_ARN` |
| `NewApiGatewayV2` | HTTP API + routes | `SST_API_<NAME>_URL` |
| `NewDynamoDB` | DynamoDB table | `SST_TABLE_<NAME>_NAME`, `SST_TABLE_<NAME>_ARN` |
| `NewBucket` | S3 bucket | `SST_BUCKET_<NAME>_NAME`, `SST_BUCKET_<NAME>_ARN` |
| `NewCron` | EventBridge schedule → Lambda | — |
| `NewQueue` | SQS queue + optional consumer | `SST_QUEUE_<NAME>_URL`, `SST_QUEUE_<NAME>_ARN` |
| `NewTopic` | SNS topic + subscribers | `SST_TOPIC_<NAME>_ARN` |
| `NewSecret` | SSM SecureString at deploy time | `SST_SECRET_<NAME>` |

### Cloudflare

Set `App.Home` to `"cloudflare"` or `"aws+cloudflare"` and provide credentials
via `CLOUDFLARE_API_TOKEN` (or `CLOUDFLARE_API_KEY` + `CLOUDFLARE_EMAIL`).

```go
App: &forge.AppConfig{
    Name: "my-app",
    Home: "aws+cloudflare",
    Cloudflare: &forge.CloudflareConfig{
        AccountID: "abc123", // or set CLOUDFLARE_ACCOUNT_ID
    },
},
```

| Construct | Package | Env vars injected |
|---|---|---|
| `NewWorker` | `constructs/cloudflare` | `SST_WORKER_<NAME>_NAME` |
| `NewKVNamespace` | `constructs/cloudflare` | `SST_KV_<NAME>_ID`, `SST_KV_<NAME>_NAME` |
| `NewD1Database` | `constructs/cloudflare` | `SST_D1_<NAME>_ID`, `SST_D1_<NAME>_NAME` |
| `NewR2Bucket` | `constructs/cloudflare` | `SST_R2_<NAME>_NAME` |

Worker bindings (KV, D1, R2 accessible as JS globals in the Worker):

```go
kv := cf.NewKVNamespace(ctx, "Cache", nil)

cf.NewWorker(ctx, "Api", &cf.WorkerArgs{
    Handler:    "../worker/index.js",
    KVBindings: []*cf.KVNamespace{kv},
})
```

---

## Multi-stage config

```go
forge.Run(&forge.Config{
    App: &forge.AppConfig{Name: "my-app", Home: "aws"},
    Stages: map[string]*forge.StageConfig{
        "production": {
            Protected:  true,      // forge remove requires --force
            AWSProfile: "prod",
            AWSRegion:  "us-east-1",
            Tags:       map[string]string{"env": "production"},
        },
    },
    Run: func(ctx *forge.RunContext) error {
        if ctx.IsProduction() {
            // production-only resources
        }
        return nil
    },
})
```

---

## Project structure

```
my-app/
├── infra/
│   ├── go.mod           ← infra module (imports forge)
│   └── sst.config.go    ← infrastructure definition
├── functions/
│   ├── api/
│   │   └── main.go      ← Lambda handler (compiled separately)
│   └── worker/
│       └── main.go
└── go.mod               ← app module (no forge/Pulumi dependency)
```

The `infra/` directory is a separate Go module so Lambda handler binaries
don't carry Pulumi as a dependency.

---

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `FORGE_STATE_BUCKET` | `<app>-<stage>-forge-state` | S3 bucket for Pulumi state |
| `FORGE_STAGE` | `$USER` or `dev` | Active stage |
| `PULUMI_CONFIG_PASSPHRASE` | `""` | State encryption passphrase |
| `AWS_PROFILE` | — | AWS credentials profile |
| `AWS_DEFAULT_REGION` | — | AWS region |
| `CLOUDFLARE_API_TOKEN` | — | Cloudflare auth (preferred) |
| `CLOUDFLARE_API_KEY` | — | Cloudflare auth (with EMAIL) |
| `CLOUDFLARE_ACCOUNT_ID` | — | Cloudflare account ID |
| `CLOUDFLARE_ZONE_ID` | — | Cloudflare zone (for Worker domains) |

---

## Roadmap

- [ ] Deploy output summary table (created / updated / deleted counts)
- [ ] Full godoc documentation
- [ ] `NextjsSite` / `StaticSite` constructs
- [ ] Fargate / ECS construct
- [ ] GitHub Actions CI integration guide
- [ ] Web console (`forge console`)

---

## License

MIT
