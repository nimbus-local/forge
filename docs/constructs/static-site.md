# StaticSite

Serves a pre-built static website from S3 via CloudFront. Suitable for Vite, Create React App, Hugo, Astro, Gatsby, and any framework that outputs a static `dist/` directory.

```go
import "github.com/sst-go/forge/constructs"

site := constructs.NewStaticSite(ctx, "Web", &constructs.StaticSiteArgs{
    Build:     "npm run build",
    OutputDir: "dist",
})
ctx.Export("url", site.URL())
```

---

## StaticSiteArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `OutputDir` | `string` | — | **Required.** Path to the pre-built asset directory (e.g. `"dist"`, `"out"`). |
| `Build` | `string` | `""` | Shell command to run before uploading (e.g. `"npm run build"`). Runs on every deploy. |
| `BuildDir` | `string` | `"."` | Working directory for the `Build` command. |
| `Environment` | `map[string]string` | nil | Env vars injected into the `Build` process. |
| `IndexDocument` | `string` | `"index.html"` | CloudFront default root object. |
| `ErrorDocument` | `string` | `"index.html"` | Served for 403/404 responses (SPA catch-all). |
| `Domain` | `string` | `""` | Custom hostname (e.g. `"www.example.com"`). Requires `DomainCertArn`. |
| `DomainCertArn` | `string` | `""` | ACM certificate ARN for `Domain`. Must be in `us-east-1`. |
| `PriceClass` | `string` | `"PriceClass_100"` | CloudFront edge network: `"PriceClass_100"` (US+EU), `"PriceClass_200"`, `"PriceClass_All"`. |

---

## Methods

```go
func (s *StaticSite) URL() pulumi.StringOutput  // CloudFront HTTPS URL
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
| `aws.s3.Bucket` | Private asset storage |
| `aws.s3.BucketPublicAccessBlock` | Block direct public access |
| `aws.cloudfront.OriginAccessControl` | Secure S3 ↔ CloudFront link (sigv4) |
| `aws.cloudfront.Distribution` | CDN with HTTPS redirect |
| `aws.s3.BucketPolicy` | Allow CloudFront OAC to read objects |
| `aws.s3.BucketObject` × N | One resource per file in `OutputDir` |

---

## Build environment variables

Static values can be injected into the build via `Environment`:

```go
site := constructs.NewStaticSite(ctx, "Web", &constructs.StaticSiteArgs{
    Build:     "npm run build",
    OutputDir: "dist",
    Environment: map[string]string{
        "VITE_API_URL":   "https://api.example.com",
        "VITE_APP_STAGE": ctx.Stage,
    },
})
```

> **Limitation:** Pulumi output values (e.g. the URL of an `ApiGatewayV2` created in the same `Run` function) cannot be injected into the build because the build runs before Pulumi creates resources. To pass a backend URL into a static build, store it in SSM after the first deploy and read it in the build command, or hard-code it for each stage.

---

## Caching strategy

| Path prefix | Cache-Control |
|---|---|
| `_next/static/`, `assets/`, `static/` | `public, max-age=31536000, immutable` |
| Everything else | `public, max-age=0, must-revalidate` |

Versioned/hashed asset paths are treated as immutable. HTML and other documents always revalidate to prevent stale content after a deploy.

---

## SPA routing

The default `ErrorDocument: "index.html"` maps all 403/404 CloudFront responses to `index.html` with HTTP 200. This is the standard setup for client-side routed SPAs (React Router, Vue Router, etc.).

To return a proper 404 page instead, set `ErrorDocument` to a dedicated page:

```go
args.ErrorDocument = "404.html"
```

---

## Custom domain

Provide an ACM certificate in `us-east-1` (required by CloudFront):

```go
site := constructs.NewStaticSite(ctx, "Web", &constructs.StaticSiteArgs{
    Build:         "npm run build",
    OutputDir:     "dist",
    Domain:        "www.example.com",
    DomainCertArn: "arn:aws:acm:us-east-1:123456789:certificate/abc-def",
})
```

After deploy, create a CNAME in your DNS provider pointing `www.example.com` to the `url` output value.

---

## Full example — Vite app + API backend

```go
package main

import (
    forge "github.com/sst-go/forge"
    "github.com/sst-go/forge/constructs"
)

func main() {
    forge.Run(&forge.Config{
        App: &forge.AppConfig{Name: "my-app", Home: "aws"},
        Run: func(ctx *forge.RunContext) error {
            // Backend API
            api := constructs.NewApiGatewayV2(ctx, "Api", nil)
            api.Route("GET /items", &constructs.RouteArgs{Handler: "functions/list/main.handler"})

            // Frontend: build first (API URL must be set separately in CI/SSM for dynamic injection)
            constructs.NewStaticSite(ctx, "Web", &constructs.StaticSiteArgs{
                Build:     "npm run build",
                OutputDir: "dist",
            })

            ctx.Export("apiUrl", api.URL())
            return nil
        },
    })
}
```
