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

## Prerequisites

- AWS credentials configured (`aws configure` or `AWS_PROFILE`)
- Node.js 18+ (the open-next build runs during `forge deploy`)
- Go 1.22+

---

## Deploy

```bash
cd infra
go mod tidy          # downloads forge + Pulumi dependencies
forge deploy         # builds Next.js via open-next, deploys to AWS
```

The deploy output prints the CloudFront URL.

To tear down:

```bash
forge remove
```

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
