# Migrating from SST v3

forge is a drop-in replacement for SST v3 Ion. If you have an existing SST project, you can migrate in minutes.

---

## Automated migration

```bash
# Inside your SST project root
forge migrate
```

This reads `sst.config.ts` and writes `infra/sst.config.go`. The migrator handles the most common patterns automatically and emits `// TODO:` comments for anything needing manual attention.

Specify an explicit input path if needed:

```bash
forge migrate path/to/sst.config.ts
```

Overwrite an existing output:

```bash
forge migrate --force
```

---

## What the migrator converts

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
| `$config({app, run})` | `forge.Run(&forge.Config{App, Run})` |
| `input?.stage === "production"` | `ctx.IsProduction()` |

### Known limitations

The migrator emits `// TODO:` comments for:

- Constructor args that span more than two lines
- Ternary expressions (`condition ? a : b`)
- `$output()` / `$interpolate()` calls — use Pulumi outputs instead
- `sst.Secret` cross-references — wire them up manually with `constructs.NewSecret`

---

## Preserving SST v3 Ion state

If your project was deployed with SST v3 Ion, its Pulumi state is stored in an S3 bucket and is fully compatible with forge — the state format is identical.

Point forge at the existing bucket:

```bash
export FORGE_STATE_BUCKET=my-app-dev-forge-state
forge diff   # should show zero changes if the migration was correct
```

Once `forge diff` shows no changes, you can start deploying with forge normally.

---

## Manual migration

If your config is too complex for the automated migrator, here is the pattern:

**Before (TypeScript):**

```typescript
export default $config({
  app(input) {
    return {
      name: "my-app",
      removal: input?.stage === "production" ? "retain" : "remove",
      home: "aws",
    };
  },
  async run() {
    const table = new sst.aws.DynamoDB("UsersTable", {
      fields: { pk: "string" },
      primaryIndex: { hashKey: "pk" },
    });

    const fn = new sst.aws.Function("Api", {
      handler: "src/index.handler",
      link: [table],
    });

    const api = new sst.aws.ApiGatewayV2("Api");
    api.route("GET /users", fn);

    return { url: api.url };
  },
});
```

**After (Go):**

```go
package main

import (
    forge "github.com/nimbus-local/forge"
    "github.com/nimbus-local/forge/constructs"
)

func main() {
    forge.Run(&forge.Config{
        App: &forge.AppConfig{
            Name:    "my-app",
            Home:    "aws",
            Removal: forge.RemovalDestroy,
        },
        Stages: map[string]*forge.StageConfig{
            "production": {Removal: forge.RemovalRetain},
        },
        Run: func(ctx *forge.RunContext) error {
            table := constructs.NewDynamoDB(ctx, "UsersTable", &constructs.DynamoDBArgs{
                PrimaryIndex: &constructs.PrimaryIndex{HashKey: "pk"},
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

## Environment variable compatibility

All `SST_*` environment variable names are preserved. Handler code that reads `SST_TABLE_MY_TABLE_NAME` works without changes.

---

## Migrating secrets

SST secrets stored in SSM Parameter Store are accessible immediately — forge uses the same path convention (`/forge/<app>/<stage>/<name>` matches SST's `/sst/<app>/<stage>/<name>`).

If the paths differ, migrate them once:

```bash
# For each secret:
forge secret set MY_SECRET "$(aws ssm get-parameter --name /sst/my-app/dev/MY_SECRET --with-decryption --query Parameter.Value --output text)"
```
