# Function

Creates an AWS Lambda function with sane defaults: ARM64 architecture, 14-day CloudWatch log retention, X-Ray tracing (in non-dev stages), and environment variable injection from linked resources.

```go
import "github.com/sst-go/forge/constructs"

fn := constructs.NewFunction(ctx, "MyFunction", &constructs.FunctionArgs{
    Handler: "bootstrap",
})
```

---

## FunctionArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Handler` | `string` | `""` | Entry point. `"bootstrap"` for compiled Go; `"src/index.handler"` for Node. |
| `Runtime` | `string` | `"provided.al2023"` | Lambda runtime identifier |
| `Architecture` | `string` | `"arm64"` | `"arm64"` (Graviton) or `"x86_64"` |
| `Timeout` | `int` | `10` | Timeout in seconds (max 900) |
| `MemorySize` | `int` | `128` | Memory in MB |
| `Environment` | `map[string]string` | nil | Static environment variables |
| `Link` | `[]forge.Linkable` | nil | Constructs whose identifiers are injected as env vars |
| `URL` | `bool` | `false` | Expose a public Lambda Function URL (no API Gateway required) |
| `Description` | `string` | `""` | Human-readable description shown in the AWS console |

---

## Methods

```go
func (f *Function) ARN() pulumi.StringOutput      // Lambda function ARN
func (f *Function) Name() pulumi.StringOutput     // Physical function name
func (f *Function) Role() *iam.Role               // IAM execution role
```

`Role()` is useful when attaching additional IAM policies:

```go
fn := constructs.NewFunction(ctx, "Worker", &constructs.FunctionArgs{...})

_, err := iam.NewRolePolicy(ctx.Pulumi(), "extra-policy", &iam.RolePolicyArgs{
    Role:   fn.Role().Name,
    Policy: pulumi.String(`{"Version":"2012-10-17","Statement":[...]}`),
})
```

---

## Linkable

`Function` implements `forge.Linkable`. Linking a function to another function injects the ARN:

| Env var | Value |
|---|---|
| `SST_FUNCTION_<NAME>_ARN` | Lambda function ARN |

---

## Compiling Go handlers

Lambda expects a compiled binary named `bootstrap` for the `provided.al2023` runtime. Build script:

```bash
GOOS=linux GOARCH=arm64 go build -o bootstrap ./functions/api
zip api.zip bootstrap
```

Or use a `Makefile`:

```makefile
build:
	GOOS=linux GOARCH=arm64 go build -o bin/bootstrap ./functions/api
```

---

## Example — Lambda with DynamoDB access

```go
table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
    PrimaryIndex: constructs.PrimaryIndex{PartitionKey: "id"},
})

fn := constructs.NewFunction(ctx, "OrdersApi", &constructs.FunctionArgs{
    Handler:    "bootstrap",
    MemorySize: 512,
    Timeout:    30,
    Link:       []forge.Linkable{table},
    Environment: map[string]string{
        "LOG_LEVEL": "info",
    },
})
```

Inside the handler, read the table name:

```go
tableName := os.Getenv("SST_TABLE_ORDERS_NAME")
```
