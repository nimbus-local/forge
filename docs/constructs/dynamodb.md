# DynamoDB

Creates a DynamoDB table with pay-per-request billing, optional GSIs, point-in-time recovery, and deletion protection.

```go
import "github.com/sst-go/forge/constructs"

table := constructs.NewDynamoDB(ctx, "Users", &constructs.DynamoDBArgs{
    Fields: map[string]constructs.FieldType{
        "userId": constructs.FieldTypeString,
    },
    PrimaryIndex: &constructs.PrimaryIndex{HashKey: "userId"},
})
```

---

## DynamoDBArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Fields` | `map[string]FieldType` | nil | Every attribute used in primary keys or indexes. Required. |
| `PrimaryIndex` | `*PrimaryIndex` | — | Required. Partition key and optional sort key. |
| `GlobalIndexes` | `[]GlobalIndex` | nil | Additional GSIs. |
| `BillingMode` | `string` | `"PAY_PER_REQUEST"` | `"PAY_PER_REQUEST"` or `"PROVISIONED"`. |
| `PointInTimeRecovery` | `bool` | `false` | Enable PITR — 35-day continuous backup window. Recommended for production. |
| `DeletionProtection` | `bool` | `false` | Prevent accidental table deletion. Recommended for production. |
| `StreamEnabled` | `bool` | `false` | Enable DynamoDB Streams. |
| `StreamViewType` | `string` | `"NEW_AND_OLD_IMAGES"` | Stream record format when `StreamEnabled` is true. |

### PrimaryIndex

```go
type PrimaryIndex struct {
    HashKey  string   // partition key attribute name
    RangeKey string   // sort key attribute name (optional)
}
```

### GlobalIndex

```go
type GlobalIndex struct {
    Name       string
    HashKey    string
    RangeKey   string   // optional
    Projection string   // "ALL" | "KEYS_ONLY" — defaults to "ALL"
}
```

### FieldType

```go
const (
    FieldTypeString FieldType = "S"
    FieldTypeNumber FieldType = "N"
    FieldTypeBinary FieldType = "B"
)
```

---

## Methods

```go
func (d *DynamoDB) Name() pulumi.StringOutput  // physical table name
func (d *DynamoDB) ARN() pulumi.StringOutput   // table ARN
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_TABLE_<NAME>_NAME` | Physical DynamoDB table name |
| `SST_TABLE_<NAME>_ARN` | Table ARN |

---

## Example — table with GSI and production safeguards

```go
table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
    Fields: map[string]constructs.FieldType{
        "customerId": constructs.FieldTypeString,
        "orderId":    constructs.FieldTypeString,
        "status":     constructs.FieldTypeString,
        "createdAt":  constructs.FieldTypeString,
    },
    PrimaryIndex: &constructs.PrimaryIndex{
        HashKey:  "customerId",
        RangeKey: "orderId",
    },
    GlobalIndexes: []constructs.GlobalIndex{
        {Name: "by-status", HashKey: "status", RangeKey: "createdAt"},
    },
    PointInTimeRecovery: ctx.Stage == "production",
    DeletionProtection:  ctx.Stage == "production",
})
```

Reading the table name inside a Lambda handler:

```go
tableName := os.Getenv("SST_TABLE_ORDERS_NAME")
```

> **Note:** SST v3 Ion does not expose `PointInTimeRecovery` or `DeletionProtection` as
> first-class fields — you would need to use its `transform` escape hatch. forge exposes
> both directly.
