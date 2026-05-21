# checklist-full

A production-shaped checklist app with GitHub authentication deployed to AWS with forge.

**Stack:** Next.js 14 + Go Lambda + DynamoDB + Auth.js (GitHub OAuth)  
**Auth:** Real login — each GitHub account gets its own private list

---

## Architecture

```
Browser
  │
  ▼
CloudFront + S3 (static assets)
  │
  ▼
Next.js SSR Lambda (open-next)
  ├─ Auth.js — handles GitHub OAuth, stores session in httpOnly cookie
  └─ /api/items proxy
       │  verifies session, adds x-user-id + x-internal-key headers
       ▼
API Gateway → Go Lambda
       │  validates headers, reads/writes DynamoDB
       ▼
DynamoDB (Items table — partitioned by GitHub user ID)
```

The Next.js server acts as a BFF (backend-for-frontend): it verifies the Auth.js session and then proxies requests to the Go Lambda with the user's GitHub ID in a trusted header. The Go Lambda never sees the browser directly and trusts `x-user-id` because it is gated by `x-internal-key` — a shared secret only the Next.js server knows.

---

## Constructs used

| Construct | Purpose |
|---|---|
| `NewDynamoDB` | Items table, partitioned by GitHub user ID |
| `NewSecret` × 4 | GitHub OAuth credentials, Auth.js secret, internal API key |
| `NewFunction` | Go Lambda — CRUD API handler |
| `NewApiGatewayV2` | HTTP API in front of the Go Lambda |
| `NewNextjsSite` | Next.js app (S3 + CloudFront + Lambda) |

---

## Prerequisites

- AWS credentials configured (`aws configure` or `AWS_PROFILE`)
- Node.js 18+ (for the open-next build)
- Go 1.22+
- A GitHub OAuth App ([create one here](https://github.com/settings/developers))
  - Homepage URL: `https://<your-cloudfront-domain>`
  - Authorization callback URL: `https://<your-cloudfront-domain>/api/auth/callback/github`

---

## First-time setup

### 1. Store secrets in SSM

```bash
cd infra
go mod tidy

forge secret set GithubId       <github-oauth-client-id>
forge secret set GithubSecret   <github-oauth-client-secret>
forge secret set NextauthSecret <openssl rand -hex 32>
forge secret set InternalKey    <openssl rand -hex 32>
```

### 2. Build the Lambda and deploy

```bash
make deploy       # compiles Go Lambda → functions/api.zip, then forge deploy
```

The deploy output will print the CloudFront URL. Update your GitHub OAuth App's callback URL if it has changed.

### 3. Tear down

```bash
make remove
```

---

## Local development

Create `web/.env.local`:

```
SST_SECRET_GITHUB_ID=<your-github-oauth-client-id>
SST_SECRET_GITHUB_SECRET=<your-github-oauth-client-secret>
SST_SECRET_NEXTAUTH_SECRET=<any-random-string>
SST_SECRET_INTERNAL_KEY=<any-random-string>
NEXTAUTH_URL=http://localhost:3000

# Point to a deployed (or locally-running) API:
SST_API_GATEWAY_URL=https://<your-deployed-api-gateway-url>
```

Then:

```bash
cd web
npm install
npm run dev
```

---

## Data model

**Table:** `checklist-full-<stage>-Items`

| Attribute | Type | Key | Description |
|---|---|---|---|
| `userId` | String | PK | GitHub user ID (from Auth.js session `token.sub`) |
| `itemId` | String | SK | UUID generated per item |
| `text` | String | — | Item label |
| `done` | Boolean | — | Checked state |
| `createdAt` | String | — | ISO 8601 timestamp |

---

## Security model

| Boundary | Mechanism |
|---|---|
| Browser → Next.js | HTTPS via CloudFront |
| Auth | Auth.js session cookie (httpOnly, secure) |
| Next.js → Go Lambda | `x-internal-key` (forge Secret, never exposed to browser) |
| User isolation | Go Lambda reads `x-user-id` set by Next.js after session verification |
| DynamoDB | `ConditionExpression: userId = :uid` guards every write and delete |
