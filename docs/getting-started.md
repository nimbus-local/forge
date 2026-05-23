# Getting Started with forge

forge is a Go-native replacement for SST (Serverless Stack). You write your infrastructure in Go, and forge deploys it to AWS (and optionally Cloudflare) using Pulumi under the hood.

---

## Prerequisites

- Go 1.22 or later
- AWS credentials configured (`aws configure` or environment variables)
- AWS account with permissions to create Lambda, API Gateway, S3, and IAM resources

---

## Installation

```bash
go install github.com/nimbus-local/forge/cmd/forge@latest
```

Verify:

```bash
forge --version
```

---

## Create your first project

```bash
forge create my-api --template go-api
cd my-api
```

This creates a project with two Go modules:

```
my-api/
├── go.mod                  ← app module (Lambda handler code)
├── functions/
│   └── api/
│       └── main.go         ← Lambda handler
└── infra/
    ├── go.mod              ← infra module (imports forge)
    └── sst.config.go       ← infrastructure definition
```

The two-module layout keeps your Lambda handler binaries free of Pulumi as a dependency — handlers stay small and compile fast.

---

## Deploy

```bash
forge deploy
```

On first run, forge automatically creates an S3 bucket for Pulumi state storage. Subsequent deploys are incremental.

The deploy output streams Pulumi's progress, then prints a summary:

```
  Changes   3 created  ·  12 unchanged

  Outputs
    url   https://abc123.execute-api.us-east-1.amazonaws.com
```

---

## Development

```bash
forge dev
```

Starts the live development tunnel. Your local Go handler processes real Lambda invocations in real time — no deploy needed for each code change.

---

## Preview changes

```bash
forge diff
```

Shows what would change on the next deploy without making any AWS API calls.

---

## Tear down

```bash
forge remove
```

Destroys all resources for the current stage. Safe to run repeatedly — idempotent.

---

## Available templates

| Template | Description |
|---|---|
| `go-api` | Lambda + API Gateway v2 |
| `go-crud` | Lambda + API Gateway v2 + DynamoDB |
| `go-worker` | Cloudflare Worker |
| `fullstack` | AWS Lambda API + Cloudflare Worker frontend |

---

## Next steps

- [Migration guide](migration.md) — converting an existing SST project
- [Config reference](config-reference.md) — full `forge.Config` documentation
- [Constructs](constructs/) — per-resource reference
- [Concepts: stages](concepts/stages.md) — multi-environment deployments
- [Concepts: linking](concepts/linking.md) — injecting resource identifiers into Lambda
- [Concepts: secrets](concepts/secrets.md) — secret management
