# KMSKey

A managed AWS KMS symmetric key that can be attached to any construct requiring encryption at rest.

## Usage

```go
key := constructs.NewKMSKey(ctx, "DataKey", nil)

// Encrypt a DynamoDB table
table := constructs.NewDynamoDB(ctx, "Users", &constructs.DynamoDBArgs{
    Fields:       map[string]constructs.FieldType{"pk": constructs.FieldTypeString},
    PrimaryIndex: &constructs.PrimaryIndex{HashKey: "pk"},
    KMSKeyArn:    key.ARN(),
})

// Encrypt a Lambda's env vars + log group
fn := constructs.NewFunction(ctx, "Api", &constructs.FunctionArgs{
    Handler:   "bootstrap",
    KMSKeyArn: key.ARN(),
})

// Encrypt an S3 bucket
bucket := constructs.NewBucket(ctx, "Uploads", &constructs.BucketArgs{
    KMSKeyArn: key.ARN(),
})

// Link the key to a Lambda so it can call KMS directly
fn2 := constructs.NewFunction(ctx, "Worker", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{key},
})
```

## Args

| Field | Type | Default | Description |
|---|---|---|---|
| `Description` | `string` | auto-generated | Human-readable key description |
| `DisableRotation` | `bool` | `false` (rotation enabled) | Set to `true` to disable automatic annual key rotation |
| `DeletionWindowInDays` | `int` | `30` | Waiting period before key deletion (7–30) |

## Env vars injected (when linked)

| Variable | Value |
|---|---|
| `SST_KMS_<NAME>_ARN` | KMS key ARN |
| `SST_KMS_<NAME>_ID` | KMS key ID |

## KMS integration on other constructs

All AWS constructs accept a `KMSKeyArn pulumi.StringInput` field. Pass `key.ARN()` to enable SSE-KMS:

| Construct | What is encrypted |
|---|---|
| `NewFunction` | Lambda env vars (`KmsKeyArn`) + CloudWatch log group (`KmsKeyId`) |
| `NewNextjsSite` | SSR Lambda env vars, server + image log groups, static assets S3 bucket |
| `NewBucket` | S3 objects (SSE-KMS via `BucketKeyEnabled: true`) |
| `NewDynamoDB` | DynamoDB table (SSE-KMS) |
| `NewQueue` | SQS messages (`KmsMasterKeyId`) |
| `NewTopic` | SNS messages (`KmsMasterKeyId`) |

For `NewFunction` and `NewNextjsSite`, a `kms:Grant` is created automatically for the Lambda execution role (operations: `GenerateDataKey`, `GenerateDataKeyWithoutPlaintext`, `Decrypt`, `DescribeKey`). For other constructs the IAM principals accessing the resource must have KMS permissions in their own policies.

### CloudWatch Logs note

Encrypting a CloudWatch log group with a customer-managed KMS key requires an additional key policy statement granting the CloudWatch Logs service principal access. Add this to your key policy before deploying:

```json
{
  "Effect": "Allow",
  "Principal": { "Service": "logs.<region>.amazonaws.com" },
  "Action": [
    "kms:Encrypt", "kms:Decrypt", "kms:ReEncrypt*",
    "kms:GenerateDataKey*", "kms:DescribeKey"
  ],
  "Resource": "*",
  "Condition": {
    "ArnEquals": {
      "kms:EncryptionContext:aws:logs:arn": "arn:aws:logs:<region>:<account>:*"
    }
  }
}
```

If using `NewKMSKey`, add this statement to the key policy via the AWS console or a separate `aws.kms.KeyPolicy` resource.

## Configurable log retention

`NewFunction` and `NewNextjsSite` both accept `LogRetentionDays int`:

| Value | Behaviour |
|---|---|
| `0` | Default — 14 days |
| `-1` | Never expire |
| Any CloudWatch-valid value | Set exactly |

Valid positive values: 1, 3, 5, 7, 14, 30, 60, 90, 120, 150, 180, 365, 400, 545, 731, 1096, 1827, 2192, 2557, 2922, 3288, 3653.
Any other non-zero positive value panics at deploy time with a helpful message.

## Configurable S3 lifecycle

`NewBucket` accepts `LifecycleDays int`:

| Value | Behaviour |
|---|---|
| `0` | No lifecycle rule |
| `> 0` | Expire current objects after N days; if `Versioning: true`, also expire noncurrent versions |
