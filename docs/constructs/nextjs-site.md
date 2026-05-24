# NextjsSite

Deploys a Next.js application to AWS using [open-next](https://open-next.js.org/).
Static assets are served from S3 via CloudFront; SSR pages and API routes run in
a Node.js 24 Lambda behind a second CloudFront origin.

```go
import "github.com/nimbus-local/forge/constructs"

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
| `aws.s3.BucketPublicAccessBlock` | Block direct public access to S3 |
| `aws.cloudfront.OriginAccessControl` | Secure S3 ↔ CloudFront (sigv4) |
| `aws.cloudfront.Function` | Viewer-request function: copies `Host` → `x-forwarded-host` |
| `aws.iam.Role` | SSR Lambda execution role |
| `aws.cloudwatch.LogGroup` | Lambda logs (14-day retention) |
| `aws.lambda.Function` | Node.js 24 SSR handler (arm64) |
| `aws.lambda.FunctionUrl` | HTTPS endpoint for CloudFront (auth type: NONE) |
| `aws.lambda.Permission` × 2 | Public invoke access (required for NONE auth) |
| `aws.iam.Role` _(optional)_ | Image optimisation Lambda execution role |
| `aws.cloudwatch.LogGroup` _(optional)_ | Image Lambda logs |
| `aws.lambda.Function` _(optional)_ | Node.js 22 image optimisation handler (arm64) |
| `aws.lambda.FunctionUrl` _(optional)_ | HTTPS endpoint for image Lambda |
| `aws.lambda.Permission` × 2 _(optional)_ | Public invoke access for image Lambda |
| `aws.cloudfront.Distribution` | CDN with S3 + Lambda origins |
| `aws.s3.BucketPolicy` | Allow CloudFront OAC to read S3 |
| `aws.s3.BucketObject` × N | One per file in `.open-next/assets/` |

Resources marked _optional_ are only created when open-next produces an
`image-optimization-function` directory (which it does by default in v3+).

---

## Lambda Function URL auth

The SSR Lambda uses `AuthorizationType: NONE` on its Function URL. This means AWS does
not require SigV4 signing to reach the Lambda — it does **not** mean the application is
unauthenticated. Application-level auth (Auth.js, GitHub OAuth, JWT middleware) runs
inside the Lambda and is completely unaffected.

Two resource-based policy statements are required for `NONE` auth type:
- `lambda:InvokeFunctionUrl` → `Principal: "*"`
- `lambda:InvokeFunction` → `Principal: "*"`

Granting only one causes AWS to show a console warning and requests will be denied.

> **Do not switch to `AWS_IAM`.** CloudFront OAC with Lambda signing does not work
> reliably and causes 403s from CloudFront.

---

## Host header forwarding

`NewNextjsSite` automatically creates a CloudFront viewer-request function that copies
the `Host` header (the CloudFront or custom domain visible to the browser) to
`x-forwarded-host` before each request is forwarded to a Lambda origin.

This means server-side code — including **next-auth** — can derive the correct public
URL from the request without a hardcoded `NEXTAUTH_URL` environment variable:

```typescript
// middleware.ts
const host = req.headers.get('x-forwarded-host') ?? req.nextUrl.host
const loginUrl = new URL('/login', `https://${host}`)
```

Custom domains work automatically; no infra config change is needed when a domain is
added or changed.

---

## Image optimisation

When open-next builds an `image-optimization-function` directory (the default in
open-next v3+), forge automatically deploys it as a second Lambda and wires a
`/_next/image*` CloudFront behaviour to it.

`next/image` components work out of the box with no extra configuration — remove
any `images: { unoptimized: true }` workaround from your `next.config.js`.

The image Lambda is granted `s3:GetObject` on the assets bucket so it can fetch
source images for optimisation.

---

## Build process

On every `forge deploy` (and `forge diff`), forge runs:

```bash
npm install
npx --yes open-next@latest build
```

in the project `Path`. The resulting `.open-next/` directory is then deployed:

| Output | Deployed to |
|---|---|
| `.open-next/assets/` | S3 bucket (static files) |
| `.open-next/server-functions/default/` | SSR Lambda (open-next v3) |
| `.open-next/server-function/` | SSR Lambda (open-next v2 fallback) |
| `.open-next/image-optimization-function/` | Image Lambda (when present) |

---

## CloudFront routing

| Request path | Routed to | Cache policy |
|---|---|---|
| `/_next/image*` | Image optimisation Lambda | `UseOriginCacheControlHeaders` |
| `/_next/static/*` | S3 | `CachingOptimized` (immutable) |
| Everything else | SSR Lambda | `CachingDisabled` (pass-through) |

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
    Fields:       map[string]constructs.FieldType{"id": constructs.FieldTypeString},
    PrimaryIndex: &constructs.PrimaryIndex{HashKey: "id"},
})

site := constructs.NewNextjsSite(ctx, "Web", &constructs.NextjsSiteArgs{
    Link: []forge.Linkable{table},
    // SST_TABLE_ORDERS_NAME and SST_TABLE_ORDERS_ARN available via process.env
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
    forge "github.com/nimbus-local/forge"
    "github.com/nimbus-local/forge/constructs"
)

func main() {
    forge.Run(&forge.Config{
        App: &forge.AppConfig{Name: "my-app", Home: "aws"},
        Stages: map[string]*forge.StageConfig{
            "production": {Protected: true, AWSProfile: "prod"},
        },
        Run: func(ctx *forge.RunContext) error {
            table := constructs.NewDynamoDB(ctx, "Orders", &constructs.DynamoDBArgs{
                Fields:       map[string]constructs.FieldType{"id": constructs.FieldTypeString},
                PrimaryIndex: &constructs.PrimaryIndex{HashKey: "id"},
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

## next-auth / Auth.js integration

With `x-forwarded-host` set automatically by the CloudFront function, configure
`authOptions` and middleware like this:

```typescript
// app/api/auth/[...nextauth]/route.ts
export const authOptions: NextAuthOptions = {
  providers: [
    GitHubProvider({
      clientId: process.env.SST_SECRET_GITHUB_ID!,
      clientSecret: process.env.SST_SECRET_GITHUB_SECRET!,
    }),
  ],
  secret: process.env.SST_SECRET_NEXTAUTH_SECRET,
  pages: { signIn: '/login' },
}
```

```typescript
// middleware.ts
import { getToken } from 'next-auth/jwt'
import { NextResponse } from 'next/server'
import type { NextRequest } from 'next/server'

export async function middleware(req: NextRequest) {
  const token = await getToken({
    req,
    secret: process.env.SST_SECRET_NEXTAUTH_SECRET ?? process.env.NEXTAUTH_SECRET,
  })
  if (!token) {
    const host = req.headers.get('x-forwarded-host') ?? req.nextUrl.host
    const loginUrl = new URL('/login', `https://${host}`)
    loginUrl.searchParams.set('callbackUrl', req.nextUrl.href)
    return NextResponse.redirect(loginUrl)
  }
  return NextResponse.next()
}
```

Set the GitHub OAuth app callback URL to `<url output>/api/auth/callback/github`.
