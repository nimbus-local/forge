# checklist-full

A production-shaped checklist app with GitHub authentication deployed to AWS with forge.

**Stack:** Next.js 16 + Go Lambda + DynamoDB + Auth.js (GitHub OAuth)  
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

The Next.js server acts as a BFF (backend-for-frontend): it verifies the Auth.js session and
then proxies requests to the Go Lambda with the user's GitHub ID in a trusted header. The Go
Lambda never sees the browser directly and trusts `x-user-id` because it is gated by
`x-internal-key` — a shared secret only the Next.js server knows.

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
- Node.js 20+ (for the open-next build)
- Go 1.22+
- forge CLI installed (`curl -fsSL https://raw.githubusercontent.com/nimbus-local/forge/master/install.sh | sh`)
- A GitHub OAuth App ([create one here](https://github.com/settings/developer_settings/apps))

---

## First-time setup

### 1. Create a GitHub OAuth App

Go to **GitHub → Settings → Developer settings → OAuth Apps → New OAuth App**.

| Field | Value |
|---|---|
| Application name | `checklist-<yourname>` (or anything) |
| Homepage URL | `https://<your-cloudfront-domain>` (update after first deploy) |
| **Authorization callback URL** | `https://<your-cloudfront-domain>/api/auth/callback/github` |

The callback URL path `/api/auth/callback/github` is **required exactly as shown** — this is
the endpoint next-auth registers to complete the OAuth flow. A mismatch (wrong path, wrong
scheme, or trailing slash) causes GitHub to return `redirect_uri_mismatch`.

Copy the **Client ID** and generate a **Client Secret**.

### 2. Update `infra/sst.config.go`

Set `NEXTAUTH_URL` to your CloudFront domain (known after the first deploy — see step 4):

```go
Environment: map[string]string{
    "NEXTAUTH_URL": "https://<your-cloudfront-domain>",
},
```

### 3. Store secrets in SSM

```bash
cd infra

forge secret set GithubId       <github-oauth-client-id>    --stage <your-stage>
forge secret set GithubSecret   <github-oauth-client-secret> --stage <your-stage>
forge secret set NextauthSecret $(openssl rand -hex 32)      --stage <your-stage>
forge secret set InternalKey    $(openssl rand -hex 32)      --stage <your-stage>
```

`--stage` defaults to your `$USER` environment variable when omitted (e.g. `tuan`).

### 4. Build the Lambda and deploy

```bash
make deploy --stage <your-stage>
```

The deploy output prints two URLs:

```
url     https://<id>.cloudfront.net   ← the Next.js site
apiUrl  https://<id>.execute-api...   ← the Go API (internal)
```

**After the first deploy:**
- Update your GitHub OAuth App's **Homepage URL** and **Authorization callback URL** with the
  printed CloudFront URL.
- Update `NEXTAUTH_URL` in `infra/sst.config.go` to the CloudFront URL, then redeploy.

### 5. Tear down

```bash
make remove
```

---

## Subsequent deploys

If you only changed the Next.js app (no infra changes):

```bash
make deploy --stage <your-stage>
```

If you only changed the Go Lambda:

```bash
make build
cd infra && forge deploy --stage <your-stage>
```

---

## Local development

Create `web/.env.local`:

```env
NEXTAUTH_URL=http://localhost:3000
SST_SECRET_GITHUB_ID=<your-github-oauth-client-id>
SST_SECRET_GITHUB_SECRET=<your-github-oauth-client-secret>
SST_SECRET_NEXTAUTH_SECRET=<any-random-string>
SST_SECRET_INTERNAL_KEY=<any-random-string>

# Point to a deployed API Gateway:
SST_API_GATEWAY_URL=https://<your-deployed-api-gateway-url>/
```

For local dev your GitHub OAuth App needs `http://localhost:3000/api/auth/callback/github`
as an additional callback URL (GitHub allows multiple).

Then:

```bash
cd web && npm install && npm run dev
```

---

## Data model

**Table:** `checklist-full-<stage>-Items-<accountId>`

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
