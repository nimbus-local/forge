# Bucket

Creates an S3 bucket. Public access is blocked by default.

```go
import "github.com/nimbus-local/forge/constructs"

bucket := constructs.NewBucket(ctx, "Uploads", nil)
```

---

## BucketArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Public` | `bool` | `false` | Allow anonymous GET access (for static assets) |
| `Versioning` | `bool` | `false` | Enable S3 object versioning |
| `CORS` | `bool` | `false` | Enable CORS for browser uploads |
| `CORSAllowOrigins` | `[]string` | `["*"]` | CORS allowed origins (when `CORS` is true) |

---

## Methods

```go
func (b *Bucket) Name() pulumi.StringOutput  // physical bucket name
func (b *Bucket) ARN() pulumi.StringOutput   // bucket ARN
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_BUCKET_<NAME>_NAME` | Physical S3 bucket name |
| `SST_BUCKET_<NAME>_ARN` | Bucket ARN |

---

## Example — CORS-enabled upload bucket

```go
bucket := constructs.NewBucket(ctx, "Uploads", &constructs.BucketArgs{
    CORS:             true,
    CORSAllowOrigins: []string{"https://example.com"},
})

fn := constructs.NewFunction(ctx, "Uploader", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{bucket},
})
```

Inside the handler:

```go
bucketName := os.Getenv("SST_BUCKET_UPLOADS_NAME")
```
