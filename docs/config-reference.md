# Config Reference

The top-level configuration passed to `forge.Run()` in your `infra/sst.config.go`.

---

## forge.Config

```go
type Config struct {
    App    *AppConfig
    Stages map[string]*StageConfig
    Run    func(ctx *RunContext) error
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `App` | `*AppConfig` | yes | Project-level metadata |
| `Stages` | `map[string]*StageConfig` | no | Per-stage overrides, keyed by stage name |
| `Run` | `func(*RunContext) error` | yes | Infrastructure definition function |

---

## forge.AppConfig

```go
type AppConfig struct {
    Name       string
    Home       string
    Removal    RemovalPolicy
    Cloudflare *CloudflareConfig
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `Name` | `string` | — | App name. Used in resource names and state bucket. Must be unique per AWS account. |
| `Home` | `string` | `"aws"` | Cloud provider: `"aws"`, `"cloudflare"`, or `"aws+cloudflare"` |
| `Removal` | `RemovalPolicy` | `RemovalDestroy` | What happens to resources on `forge remove` |
| `Cloudflare` | `*CloudflareConfig` | nil | Cloudflare account settings (required when `Home` includes cloudflare) |

### RemovalPolicy

| Value | Behaviour |
|---|---|
| `forge.RemovalDestroy` | Resources are deleted (default) |
| `forge.RemovalRetain` | Resources are kept in AWS after `forge remove` |
| `forge.RemovalRetainOnProtection` | Resources with deletion protection enabled are kept |

---

## forge.StageConfig

Per-stage overrides. Applied on top of `AppConfig` when the matching stage is active.

```go
type StageConfig struct {
    Removal    RemovalPolicy
    AWSProfile string
    AWSRegion  string
    Protected  bool
    Tags       map[string]string
}
```

| Field | Description |
|---|---|
| `Removal` | Override the base removal policy for this stage |
| `AWSProfile` | Use a different AWS credentials profile (`~/.aws/credentials`) |
| `AWSRegion` | Deploy this stage to a different AWS region |
| `Protected` | When true, `forge remove` requires `--force` |
| `Tags` | Extra resource tags merged into every resource in this stage |

**Example — production stage:**

```go
Stages: map[string]*forge.StageConfig{
    "production": {
        Protected:  true,
        AWSProfile: "prod",
        AWSRegion:  "us-east-1",
        Tags:       map[string]string{"env": "production", "cost-center": "backend"},
    },
},
```

---

## forge.CloudflareConfig

```go
type CloudflareConfig struct {
    AccountID string
    ZoneID    string
}
```

| Field | Env var fallback | Description |
|---|---|---|
| `AccountID` | `CLOUDFLARE_ACCOUNT_ID` | Cloudflare account ID (required for all CF resources) |
| `ZoneID` | `CLOUDFLARE_ZONE_ID` | Cloudflare zone ID (required for Worker custom domains) |

---

## forge.RunContext

Passed to your `Config.Run` function. Use it to create constructs and export outputs.

### Methods

```go
// Returns true when Stage is "production" or "prod".
func (r *RunContext) IsProduction() bool

// Returns true if Stage matches any of the provided names.
func (r *RunContext) StageIn(stages ...string) bool

// Returns the extra tags configured in the active StageConfig.
func (r *RunContext) ExtraTags() map[string]string

// Returns the underlying *pulumi.Context for advanced use cases.
func (r *RunContext) Pulumi() *pulumi.Context

// Exports a value as a stack output visible after deploy.
func (r *RunContext) Export(name string, value interface{})
```

### Fields

```go
Stage       string      // active stage name ("dev", "production", etc.)
App         *AppConfig  // the resolved app config
DevMode     bool        // true when running forge dev
IsProtected bool        // true when the active stage has Protected: true
```

---

## forge.Linkable

The interface implemented by every construct that can inject environment variables into a Lambda.

```go
type Linkable interface {
    LinkEnv() pulumi.StringMap
    LinkName() string
}
```

When you set `Link: []forge.Linkable{table, bucket}` on a `FunctionArgs`, forge merges the `LinkEnv()` output of each linked resource into the Lambda's environment variables at deploy time.

See [concepts/linking.md](concepts/linking.md) for details.

---

## Full example

```go
package main

import (
    forge "github.com/nimbus-local/forge"
    "github.com/nimbus-local/forge/constructs"
)

func main() {
    forge.Run(&forge.Config{
        App: &forge.AppConfig{
            Name:    "acme-backend",
            Home:    "aws",
            Removal: forge.RemovalDestroy,
        },
        Stages: map[string]*forge.StageConfig{
            "production": {
                Protected:  true,
                AWSProfile: "acme-prod",
                AWSRegion:  "eu-west-1",
                Removal:    forge.RemovalRetain,
            },
        },
        Run: func(ctx *forge.RunContext) error {
            table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
                PrimaryIndex: &constructs.PrimaryIndex{HashKey: "id"},
            })

            fn := constructs.NewFunction(ctx, "Api", &constructs.FunctionArgs{
                Handler:    "bootstrap",
                MemorySize: 256,
                Timeout:    30,
                Link:       []forge.Linkable{table},
            })

            api := constructs.NewApiGatewayV2(ctx, "Api", nil)
            api.Route("GET /orders", &constructs.RouteArgs{Function: fn})
            api.Route("POST /orders", &constructs.RouteArgs{Function: fn})

            ctx.Export("url", api.URL())
            return nil
        },
    })
}
```
