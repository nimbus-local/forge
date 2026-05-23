# Topic

Creates an SNS topic with optional Lambda subscribers.

```go
import "github.com/nimbus-local/forge/constructs"

topic := constructs.NewTopic(ctx, "Events", nil)
```

---

## TopicArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Subscribers` | `[]*FunctionArgs` | nil | Lambda functions subscribed to every message |
| `FIFO` | `bool` | `false` | Create a FIFO topic with content-based deduplication (`.fifo` suffix added automatically) |

---

## Methods

```go
func (t *Topic) ARN() pulumi.StringOutput  // topic ARN
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_TOPIC_<NAME>_ARN` | SNS topic ARN |

---

## Example — event fan-out with two subscribers

```go
topic := constructs.NewTopic(ctx, "OrderEvents", &constructs.TopicArgs{
    Subscribers: []*constructs.FunctionArgs{
        {Handler: "bootstrap", Timeout: 30},
        {Handler: "bootstrap", Timeout: 30},
    },
})

// Another function publishes to the topic
publisher := constructs.NewFunction(ctx, "OrderCreator", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{topic},
})
```

Inside the publisher:

```go
topicARN := os.Getenv("SST_TOPIC_ORDEREVENTS_ARN")
```
