# Stages

Every forge deployment targets a named stage. A stage isolates all infrastructure so different environments (dev, staging, production) never share resources.

---

## How stages work

All physical resource names are prefixed with `<appName>-<stage>-`:

```
my-app-prod-UsersTable
my-app-staging-UsersTable
my-app-dev-UsersTable   ← completely separate DynamoDB table
```

The Pulumi state bucket is also stage-specific:
```
s3://my-app-prod-forge-state
s3://my-app-dev-forge-state
```

---

## Specifying a stage

```bash
forge deploy --stage prod
forge deploy --stage staging
forge deploy              # defaults to the "dev" stage
```

The active stage is available in `RunContext.Stage`:

```go
Run: func(ctx *forge.RunContext) error {
    fmt.Println("Deploying stage:", ctx.Stage)
    return nil
},
```

---

## Per-stage configuration

Use the `Stages` map in `Config` to override settings per stage:

```go
forge.Run(&forge.Config{
    App: &forge.AppConfig{
        Name:    "myapp",
        Home:    "aws",
        Removal: forge.RemovalPolicyRetain,
    },
    Stages: map[string]*forge.StageConfig{
        "prod": {
            Removal:    forge.RemovalPolicyRetain,
            Protected:  true,            // forge remove requires --force
            AWSProfile: "prod-account",
            AWSRegion:  "us-east-1",
            Tags:       map[string]string{"env": "production"},
        },
        "staging": {
            Removal:   forge.RemovalPolicyDestroy,
            AWSRegion: "us-west-2",
        },
    },
    Run: func(ctx *forge.RunContext) error {
        // ...
        return nil
    },
})
```

### StageConfig fields

| Field | Type | Description |
|---|---|---|
| `Removal` | `RemovalPolicy` | Override the app-level removal policy for this stage |
| `AWSProfile` | `string` | AWS credentials profile for this stage |
| `AWSRegion` | `string` | Target AWS region for this stage |
| `Protected` | `bool` | Require `--force` for `forge remove` |
| `Tags` | `map[string]string` | Extra tags merged onto every resource |

---

## Branching on stage in code

```go
Run: func(ctx *forge.RunContext) error {
    if ctx.IsProduction() {
        // extra safeguards for prod
    }

    if ctx.StageIn("staging", "prod") {
        // shared between staging and prod
    }

    // Conditional resource based on stage
    if ctx.IsProduction() {
        constructs.NewDynamoDB(ctx, "AuditLog", &constructs.DynamoDBArgs{
            PrimaryIndex:        &constructs.PrimaryIndex{HashKey: "id"},
            PointInTimeRecovery: true,
        })
    }
    return nil
},
```

---

## Listing deployed stages

```bash
forge stages
```

Output:

```
STAGE       LAST DEPLOYED        RESOURCES   PROTECTED
prod        2024-09-15 14:23     42          yes
staging     2024-09-14 10:01     38          no
dev         2024-09-13 09:45     35          no
```

---

## Protected stages

Set `Protected: true` to prevent accidental teardown:

```go
Stages: map[string]*forge.StageConfig{
    "prod": {Protected: true},
},
```

```bash
forge remove --stage prod
# Error: stage "prod" is protected — use --force to proceed

forge remove --stage prod --force
# proceeds
```

---

## Personal dev stages

Convention: use your username as the stage name for a personal sandbox:

```bash
forge deploy --stage alice
forge deploy --stage bob
```

Each developer gets isolated resources with no collision risk.
