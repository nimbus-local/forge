# DynamoDB

Creates a DynamoDB table with pay-per-request billing, optional GSIs, and point-in-time recovery.

```go
import "github.com/sst-go/forge/constructs"

table := constructs.NewDynamoDB(ctx, "Users", &constructs.DynamoDBArgs{
    PrimaryIndex: constructs.PrimaryIndex{PartitionKey: "id"},
})
```

---

## DynamoDBArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `PrimaryIndex` | `PrimaryIndex` | — | Required. Partition key and optional sort key. |
| `GlobalIndexes` | `[]GlobalIndex` | nil | Additional GSIs |
| `PointInTimeRecovery` | `bool` | `false` | Enable PITR backups |

### PrimaryIndex

```go
type PrimaryIndex struct {
    PartitionKey string   // attribute name for the partition key (hash key)
    SortKey      string   // optional attribute name for the sort key (range key)
}
```

### GlobalIndex

```go
type GlobalIndex struct {
    Name         string
    PartitionKey string
    SortKey      string   // optional
}
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

When you link a table to a function, forge also attaches an IAM policy granting the function full DynamoDB access to that table.

---

## Example — table with GSI

```go
table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
    PrimaryIndex: constructs.PrimaryIndex{
        PartitionKey: "customerId",
        SortKey:      "orderId",
    },
    GlobalIndexes: []constructs.GlobalIndex{
        {Name: "by-status", PartitionKey: "status", SortKey: "createdAt"},
    },
    PointInTimeRecovery: true,
})
```

Reading the table name inside a Lambda handler:

```go
tableName := os.Getenv("SST_TABLE_ORDERS_NAME")
```
