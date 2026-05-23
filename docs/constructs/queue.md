# Queue

Creates an SQS queue with optional Lambda consumer and dead-letter queue.

```go
import "github.com/nimbus-local/forge/constructs"

queue := constructs.NewQueue(ctx, "Jobs", &constructs.QueueArgs{
    Consumer: &constructs.FunctionArgs{
        Handler: "bootstrap",
    },
})
```

---

## QueueArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Consumer` | `*FunctionArgs` | nil | Lambda that processes messages. If nil, no consumer is created. |
| `Fifo` | `bool` | `false` | Create a FIFO queue (`.fifo` suffix added automatically) |
| `VisibilityTimeout` | `int` | `30` | Seconds a message is hidden from other consumers after being received |
| `BatchSize` | `int` | `10` | Number of messages delivered to the consumer per invocation |
| `DeadLetterQueue` | `bool` | `false` | Create a sibling DLQ (max receive count: 3, retention: 14 days) |

---

## Methods

```go
func (q *Queue) URL() pulumi.StringOutput    // queue URL
func (q *Queue) ARN() pulumi.StringOutput    // queue ARN
func (q *Queue) DLQURL() pulumi.StringOutput // DLQ URL (empty string if no DLQ)
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_QUEUE_<NAME>_URL` | SQS queue URL |
| `SST_QUEUE_<NAME>_ARN` | Queue ARN |

---

## Example — FIFO queue with DLQ

```go
queue := constructs.NewQueue(ctx, "Orders", &constructs.QueueArgs{
    Fifo:              true,
    VisibilityTimeout: 60,
    BatchSize:         1,
    DeadLetterQueue:   true,
    Consumer: &constructs.FunctionArgs{
        Handler: "bootstrap",
        Timeout: 55,
    },
})

// Another function sends to the queue
sender := constructs.NewFunction(ctx, "OrderCreator", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{queue},
})
```

The sender reads the queue URL:

```go
queueURL := os.Getenv("SST_QUEUE_ORDERS_URL")
```
