# NextjsSite

Deploys a Next.js application to AWS using [open-next](https://open-next.js.org/).
Static assets are served from S3 via CloudFront; SSR pages and API routes run in
a Node.js 20 Lambda behind a second CloudFront origin.

```go
import "github.com/sst-go/forge/constructs"

site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
    Path: ".",
    Link: []forge.Linkable{table},
})
ctx.Export("url", site.URL())
```

---

## Prerequisites

Install open-next in your Next.js project:

```bash
npm install --save-dev open-next
```

No changes to `next.config.js` are required. open-next handles all bundling.

---

## NextjsSiteArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Path` | `string` | `"."` | Root of the Next.js project directory. |
| `Environment` | `map[string]string` | nil | Env vars injected into the open-next build AND the Lambda runtime. |
| `Link` | `[]forge.Linkable` | nil | Resources whose `SST_*` env vars are injected into the Lambda at deploy time. |
| `MemorySize` | `int` | `1024` | SSR Lambda memory in MB. |
| `Timeout` | `int` | `30` | SSR Lambda timeout in seconds. |
| `Domain` | `string` | `""` | Custom hostname. Requires `DomainCertArn`. |
| `DomainCertArn` | `string` | `""` | ACM certificate ARN for `Domain`. Must be in `us-east-1`. |
| `PriceClass` | `string` | `"PriceClass_100"` | CloudFront edge network: `"PriceClass_100"` (US+EU), `"PriceClass_200"`, `"PriceClass_All"`. |

---

## Methods

```go
func (n *NextjsSite) URL() pulumi.StringOutput  // CloudFront HTTPS URL
func (n *NextjsSite) Role() *iam.Role           // IAM execution role for the SSR Lambda
```

`Role()` lets you attach additional IAM policies:

```go
site := constructs.NewNextjsSite(ctx, "Web", args)

iam.NewRolePolicy(ctx.Pulumi(), "web-dynamo-policy", &iam.RolePolicyArgs{
    Role:   site.Role().Name,
    Policy: pulumi.String(`{"Version":"2012-10-17","Statement":[...]}`),
})
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_SITE_<NAME>_URL` | CloudFront HTTPS URL |

---

## Resources created

| Resource | Purpose |
|---|---|
| `aws.s3.Bucket` | Static asset storage |
| `aws.s3.BucketPublicAccessBlock` | Block direct public access |
| `aws.cloudfront.OriginAccessControl` | Secure S3 ↔ CloudFront (sigv4) |
| `aws.iam.Role` | SSR Lambda execution role |
| `aws.cloudwatch.LogGroup` | Lambda logs (14-day retention) |
| `aws.lambda.Function` | Node.js 20 SSR handler |
| `aws.lambda.FunctionUrl` | HTTPS endpoint for CloudFront |
| `aws.cloudfront.Distribution` | CDN with two origins |
| `aws.s3.BucketPolicy` | Allow CloudFront OAC |
| `aws.s3.BucketObject` × N | One per file in `.open-next/assets/` |

---

## Build process

On every `forge deploy` (and `forge diff`), forge runs:

```bash
npx --yes open-next@latest build
```

in the project `Path`. The resulting `.open-next/` directory is then deployed:

| Output | Deployed to |
|---|---|
| `.open-next/assets/` | S3 bucket (static files) |
| `.open-next/server-function/` | Lambda function (zipped by Pulumi) |

---

## CloudFront routing

| Request path | Routed to | Cache policy |
|---|---|---|
| `/_next/static/*` | S3 | `CachingOptimized` (immutable) |
| Everything else | Lambda Function URL | `CachingDisabled` (pass-through) |

---

## Environment variables

Variables in `Environment` are injected into both the open-next build and the Lambda runtime:

```go
site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
    Environment: map[string]string{
        "NEXT_PUBLIC_STAGE": ctx.Stage,
    },
})
```

`NEXT_PUBLIC_*` variables are inlined into the client bundle by Next.js. Server-only values (without the `NEXT_PUBLIC_` prefix) are only available in the Lambda runtime.

Linked resource env vars (`SST_*`) are injected **only at Lambda runtime** (not at build time):

```go
table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
    PrimaryIndex: constructs.PrimaryIndex{PartitionKey: "id"},
})

site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
    Link: []forge.Linkable{table},
    // table.LinkEnv() → SST_TABLE_ORDERS_NAME, SST_TABLE_ORDERS_ARN
    // available in server components and API routes via process.env
})
```

---

## Custom domain

```go
site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
    Path:          ".",
    Domain:        "www.example.com",
    DomainCertArn: "arn:aws:acm:us-east-1:123456789:certificate/abc-def",
})
```

After deploy, add a CNAME pointing `www.example.com` → the `url` output.

---

## Full example

```go
package main

import (
    forge "github.com/sst-go/forge"
    "github.com/sst-go/forge/constructs"
)

func main() {
    forge.Run(&forge.Config{
        App: &forge.AppConfig{Name: "my-app", Home: "aws"},
        Stages: map[string]*forge.StageConfig{
            "production": {Protected: true, AWSProfile: "prod"},
        },
        Run: func(ctx *forge.RunContext) error {
            table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
                PrimaryIndex: constructs.PrimaryIndex{PartitionKey: "id"},
            })

            site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
                Path: ".",
                Link: []forge.Linkable{table},
                Environment: map[string]string{
                    "NEXT_PUBLIC_STAGE": ctx.Stage,
                },
            })

            ctx.Export("url", site.URL())
            return nil
        },
    })
}
```

---

## Limitations

- Image optimisation (`next/image` with a remote loader) uses the same SSR Lambda. For high-traffic image workloads, consider an external image CDN.
- `middleware.ts` edge functions are not deployed to CloudFront edge; they run in the Lambda. Latency-sensitive middleware may benefit from Lambda@Edge (not currently supported by forge).
- The `Environment` map cannot contain Pulumi output values at build time. Use `Link` to inject dynamic resource identifiers at Lambda runtime.
