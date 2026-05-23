# Linking

Linking is how forge passes resource identifiers (URLs, ARNs, names) between constructs at deploy time — without hard-coding values.

---

## How it works

Every linkable construct implements the `Linkable` interface:

```go
type Linkable interface {
    LinkEnv() pulumi.StringMap  // env vars to inject
    LinkName() string           // construct name (for debugging)
}
```

When a `Function` is created with `Link: []forge.Linkable{...}`, forge merges every `LinkEnv()` map into the Lambda's environment variables. Values are resolved by Pulumi at deploy time, so they always reflect the actual deployed resource.

---

## Example

```go
table  := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
    PrimaryIndex: &constructs.PrimaryIndex{HashKey: "id"},
})
bucket := constructs.NewBucket(ctx, "Uploads", nil)
queue  := constructs.NewQueue(ctx, "Jobs", nil)

fn := constructs.NewFunction(ctx, "Api", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{table, bucket, queue},
})
```

The Lambda environment will contain:

```
SST_TABLE_ORDERS_NAME   = my-app-prod-Orders
SST_TABLE_ORDERS_ARN    = arn:aws:dynamodb:...
SST_BUCKET_UPLOADS_NAME = my-app-prod-uploads-a1b2c3
SST_BUCKET_UPLOADS_ARN  = arn:aws:s3:::my-app-prod-uploads-a1b2c3
SST_QUEUE_JOBS_URL      = https://sqs.us-east-1.amazonaws.com/...
SST_QUEUE_JOBS_ARN      = arn:aws:sqs:...
```

---

## Reading linked values in handlers

```go
import "os"

func handler(ctx context.Context, event events.APIGatewayV2HTTPRequest) (events.APIGatewayV2HTTPResponse, error) {
    tableName := os.Getenv("SST_TABLE_ORDERS_NAME")
    bucketName := os.Getenv("SST_BUCKET_UPLOADS_NAME")
    queueURL   := os.Getenv("SST_QUEUE_JOBS_URL")
    // ...
}
```

---

## Env var naming convention

The naming convention matches SST exactly, so handler code is portable between SST and forge:

```
SST_<TYPE>_<SCREAMING_SNAKE_NAME>_<ATTRIBUTE>
```

| Type | Example construct name | Env var |
|---|---|---|
| `TABLE` | `MyOrders` | `SST_TABLE_MY_ORDERS_NAME` |
| `BUCKET` | `UserUploads` | `SST_BUCKET_USER_UPLOADS_NAME` |
| `QUEUE` | `JobQueue` | `SST_QUEUE_JOB_QUEUE_URL` |
| `TOPIC` | `OrderEvents` | `SST_TOPIC_ORDER_EVENTS_ARN` |
| `API` | `PublicApi` | `SST_API_PUBLIC_API_URL` |
| `FUNCTION` | `Processor` | `SST_FUNCTION_PROCESSOR_ARN` |
| `SECRET` | `DbPassword` | `SST_SECRET_DB_PASSWORD` |
| `KV` | `Sessions` | `SST_KV_SESSIONS_ID` |
| `D1` | `UsersDb` | `SST_D1_USERS_DB_ID` |
| `R2` | `MediaFiles` | `SST_R2_MEDIA_FILES_NAME` |
| `WORKER` | `EdgeApi` | `SST_WORKER_EDGE_API_NAME` |

Name conversion: `camelCase` and `kebab-case` both become `SCREAMING_SNAKE_CASE`.

---

## IAM permissions

Linking a resource to a Function also grants the function's IAM role the necessary permissions:

| Linked resource | IAM permissions granted |
|---|---|
| DynamoDB table | Full DynamoDB access on that table |
| S3 bucket | `s3:GetObject`, `s3:PutObject`, `s3:DeleteObject`, `s3:ListBucket` |
| SQS queue | `sqs:SendMessage`, `sqs:ReceiveMessage`, `sqs:DeleteMessage`, `sqs:GetQueueAttributes` |
| SNS topic | `sns:Publish` |
| Secret | `ssm:GetParameter` on that SSM path |

---

## Linking across constructs

You can link any `Linkable` to any `Function`. This includes constructs from different parts of your infra:

```go
// Cron job that reads from a table and writes to a queue
cron := constructs.NewCron(ctx, "Sync", &constructs.CronArgs{
    Schedule: "rate(1 hour)",
    Job: &constructs.FunctionArgs{
        Handler: "bootstrap",
        Link:    []forge.Linkable{table, queue},
    },
})
```

---

## Cloudflare Workers

Workers receive linked resources as plain-text bindings (accessible as `env.*`):

```go
apiGW := constructs.NewApiGatewayV2(ctx, "Api", nil)

worker := cf.NewWorker(ctx, "Frontend", &cf.WorkerArgs{
    Handler: "worker/index.ts",
    Link:    []forge.Linkable{apiGW},
})
```

Inside the worker:

```ts
const apiURL = env.SST_API_API_URL;
const response = await fetch(`${apiURL}/users`);
```
