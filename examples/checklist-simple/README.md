# checklist-simple

A minimal checklist app deployed to AWS with forge.

**Stack:** Next.js 14 (App Router) + DynamoDB  
**Auth:** None — lists are anonymous and cookie-keyed (see below)

---

## Why anonymous?

Adding authentication (GitHub OAuth, Cognito) would require additional services and pre-deploy configuration that obscures the core forge workflow. This example deliberately keeps the auth surface to zero so you can focus on:

- How a DynamoDB table is linked to a Next.js site
- How `SST_TABLE_*` environment variables reach the Next.js server Lambda
- How to attach an IAM policy to the `NextjsSite` role

### Cookie-keyed lists

When a user adds their first item, the API route generates a random UUID and writes it to an `httpOnly` cookie (`userId`). Every subsequent read and write is scoped to that UUID.

| Property | Behaviour |
|---|---|
| Privacy | Private by obscurity — a stranger needs your UUID to see your list |
| Persistence | Tied to the browser cookie; clearing cookies starts a fresh list |
| Sharing | Not supported — there is no login, so lists cannot be shared |

This is the right trade-off for a getting-started example. For an authenticated version with per-user lists, GitHub OAuth, and a Go API Lambda, see [`../checklist-full`](../checklist-full).

---

## Constructs used

| Construct | Purpose |
|---|---|
| `NewDynamoDB` | Stores checklist items keyed by `userId` + `itemId` |
| `NewNextjsSite` | Deploys the Next.js app (S3 + CloudFront + Lambda@Edge) |

---

## Deploy

### Prerequisites

- AWS credentials configured (`aws configure` or `AWS_PROFILE`)
- Go 1.22+
- Node.js 18+ (Next.js build toolchain — the SSR Lambda runs on `nodejs20.x`)
- forge CLI: `go install github.com/nimbus-local/forge/cmd/forge@latest`

Pulumi is downloaded automatically on first deploy to `~/.forge/pulumi/` — no separate install needed.

### Commands

```bash
make deploy          # bootstrap state bucket + deploy (STAGE=dev by default)
make deploy STAGE=production
make remove          # tear down
make dev             # run Next.js locally
```

`make deploy` handles the full sequence: `go mod tidy`, state bucket creation
(idempotent), and `forge deploy`. The CloudFront URL is printed when it finishes.

CloudFront distributions take ~1–2 minutes to become globally active after first deploy.

---

## Local development

```bash
cd web
npm install
npm run dev
```

For local dev the API routes use DynamoDB. Set the table name in `web/.env.local`:

```
SST_TABLE_ITEMS_NAME=checklist-simple-dev-Items   # replace with your deployed table name
```

Then configure AWS credentials in the terminal so the dev server can reach DynamoDB.

---

## Data model

**Table:** `checklist-simple-<stage>-Items`

| Attribute | Type | Key | Description |
|---|---|---|---|
| `userId` | String | PK | Anonymous browser UUID stored in cookie |
| `itemId` | String | SK | UUID generated per item |
| `text` | String | — | Item label |
| `done` | Boolean | — | Checked state |
| `createdAt` | String | — | ISO 8601 timestamp |
