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
| Migration tool | — | ✓ `forge migrate` |
| Node.js required | ✓ | ✗ |
| Multi-stage | ✓ | ✓ |
| AWS target | ✓ | ✓ |
| Cloudflare target | ✓ | roadmap |

---

## Installation

```bash
go install github.com/sst-go/forge/cmd/forge@latest
```

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

### 3. Preview before deploying

```bash
forge diff --stage dev      # same as `sst diff`
```

### 4. Deploy

```bash
forge deploy --stage dev
forge deploy --stage production
```

### State file compatibility

If you were already on **SST v3 Ion**, your Pulumi state is in S3 and is fully
compatible. Set `FORGE_STATE_BUCKET` to the same bucket SST was using:

```bash
export FORGE_STATE_BUCKET=my-app-dev-forge-state   # same bucket as SST
forge diff   # should show zero changes if config was migrated correctly
```

---

## Writing a config from scratch

Create `infra/sst.config.go`:

```go
package main

import (
    "github.com/sst-go/forge"
    "github.com/sst-go/forge/constructs"
)

func main() {
    forge.Run(&forge.Config{
        App: &forge.AppConfig{
            Name: "my-app",
            Home: "aws",
        },
        Run: func(ctx *forge.RunContext) error {
            table := constructs.NewDynamoDB(ctx, "UsersTable", &constructs.DynamoDBArgs{
                Fields:       map[string]constructs.FieldType{"pk": constructs.FieldTypeString},
                PrimaryIndex: &constructs.PrimaryIndex{HashKey: "pk"},
            })

            api := constructs.NewApiGatewayV2(ctx, "Api", nil)
            api.Route("GET /users", &constructs.RouteArgs{
                Handler: "functions/list/main.handler",
                Link:    []forge.Linkable{table},
            })

            ctx.Export("apiUrl", api.URL())
            return nil
        },
    })
}
```

Then create `infra/go.mod`:

```
module my-app/infra

go 1.22

require github.com/sst-go/forge v0.1.0
```

---

## Commands

```
forge deploy [--stage <stage>]    Deploy your stack
forge dev    [--stage <stage>]    Live Lambda dev tunnel
forge diff   [--stage <stage>]    Preview changes (no deploy)
forge remove [--stage <stage>]    Destroy a stage
forge secret set   <NAME> <VAL>   Store a secret in SSM
forge secret get   <NAME>         Retrieve a secret
forge secret remove <NAME>        Delete a secret
forge secret list                 List all secrets for stage
forge migrate [sst.config.ts]     Convert TS config to Go
```

### Global flags

```
--stage, -s   Deployment stage (default: $USER or "dev")
--profile     AWS credentials profile
--region      AWS region
--config      Path to sst.config.go (default: ./infra/sst.config.go)
```

---

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `FORGE_STATE_BUCKET` | `<app>-<stage>-forge-state` | S3 bucket for Pulumi state |
| `FORGE_STAGE` | `$USER` or `dev` | Active stage |
| `PULUMI_CONFIG_PASSPHRASE` | `""` | State encryption passphrase |
| `AWS_PROFILE` | — | AWS credentials profile |
| `AWS_DEFAULT_REGION` | — | AWS region |

---

## Constructs

All constructs inject their ARNs / URLs into any Function they're linked to:

| Construct | Env vars injected |
|---|---|
| `DynamoDB` | `SST_TABLE_<NAME>_NAME`, `SST_TABLE_<NAME>_ARN` |
| `Bucket` | `SST_BUCKET_<NAME>_NAME`, `SST_BUCKET_<NAME>_ARN` |
| `ApiGatewayV2` | `SST_API_<NAME>_URL` |
| `Function` | `SST_FUNCTION_<NAME>_ARN` |

---

## Project structure

```
my-app/
├── infra/
│   ├── go.mod           ← infra module (imports forge)
│   └── sst.config.go    ← infrastructure definition
├── functions/
│   ├── list/
│   │   └── main.go      ← Lambda handler (separate binary)
│   └── create/
│       └── main.go
└── go.mod               ← app module (no forge dependency)
```

The `infra/` directory is a separate Go module so your Lambda handler binaries
don't pull in Pulumi as a dependency.

---

## Roadmap

- [ ] Cloudflare Workers support
- [ ] `NextjsSite` / `StaticSite` constructs
- [ ] Cron / EventBridge construct
- [ ] SQS Queue / SNS Topic constructs
- [ ] Web console (`forge console`)
- [ ] GitHub Actions / CI integration guide
- [ ] Fargate / ECS construct

---

## License

MIT
