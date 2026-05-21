# GitHub Actions CI/CD

This guide covers deploying with forge from GitHub Actions. It uses OIDC (no long-lived AWS credentials), shows patterns for multi-stage pipelines, preview deployments on pull requests, and secret management.

---

## Prerequisites

### 1. Add the GitHub OIDC provider to AWS

Run once per AWS account:

```bash
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1
```

### 2. Create a deploy IAM role

Create a role that GitHub Actions can assume. Replace `123456789012` with your account ID and `my-org/my-repo` with your repository.

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {
      "Federated": "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
    },
    "Action": "sts:AssumeRoleWithWebIdentity",
    "Condition": {
      "StringEquals": {
        "token.actions.githubusercontent.com:aud": "sts.amazonaws.com"
      },
      "StringLike": {
        "token.actions.githubusercontent.com:sub": "repo:my-org/my-repo:*"
      }
    }
  }]
}
```

Attach `AdministratorAccess` (or a scoped policy covering your constructs) to the role.

Store the role ARN in a **GitHub Actions variable** (not a secret — it's not sensitive):

```
Repository settings → Secrets and variables → Actions → Variables
Name: AWS_DEPLOY_ROLE_ARN
Value: arn:aws:iam::123456789012:role/forge-deploy
```

### 3. Set the Pulumi passphrase

forge uses Pulumi's S3 backend with an encrypted state file. Set a stable passphrase:

```
Repository settings → Secrets → Actions
Name: PULUMI_CONFIG_PASSPHRASE
Value: <any stable string — you choose it once and keep it>
```

> Using an empty string (`""`) is valid for non-sensitive state files (resource IDs, outputs). If you store secrets via `constructs.NewSecret`, those values are encrypted in SSM — only metadata ends up in Pulumi state.

---

## Installing forge in CI

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: '1.24'
    cache: true

- name: Install forge
  run: go install github.com/sst-go/forge/cmd/forge@latest
```

Or pin to a specific version:

```yaml
- run: go install github.com/sst-go/forge/cmd/forge@v0.3.0
```

---

## Basic: deploy on push to main

```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  push:
    branches: [main]

permissions:
  id-token: write   # required for OIDC
  contents: read

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true

      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.AWS_DEPLOY_ROLE_ARN }}
          aws-region: us-east-1

      - run: go install github.com/sst-go/forge/cmd/forge@latest

      - name: Deploy to prod
        run: forge deploy --stage prod
        env:
          PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}
```

---

## Multi-stage pipeline

A common pattern: `main` → staging, git tag → prod. Both stages share the same workflow file.

```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  push:
    branches: [main]
    tags: ['v*']

permissions:
  id-token: write
  contents: read

jobs:
  deploy-staging:
    if: github.ref == 'refs/heads/main'
    runs-on: ubuntu-latest
    environment: staging
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.AWS_DEPLOY_ROLE_ARN }}
          aws-region: us-east-1
      - run: go install github.com/sst-go/forge/cmd/forge@latest
      - run: forge deploy --stage staging
        env:
          PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}

  deploy-prod:
    if: startsWith(github.ref, 'refs/tags/v')
    runs-on: ubuntu-latest
    environment: production   # triggers GitHub's manual approval gate if configured
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.AWS_DEPLOY_ROLE_ARN }}
          aws-region: us-east-1
      - run: go install github.com/sst-go/forge/cmd/forge@latest
      - run: forge deploy --stage prod
        env:
          PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}
```

> **Tip:** In GitHub, go to _Settings → Environments → production_ and enable **Required reviewers** to add a manual approval gate before `forge deploy --stage prod` runs.

---

## Preview deployments on pull requests

Deploy each PR to its own ephemeral stage (`pr-<number>`), post the outputs as a PR comment, and tear down automatically when the PR closes.

### Deploy on PR open / update

```yaml
# .github/workflows/preview.yml
name: Preview

on:
  pull_request:
    types: [opened, synchronize, reopened]

permissions:
  id-token: write
  contents: read
  pull-requests: write   # to post comments

jobs:
  preview:
    runs-on: ubuntu-latest
    env:
      STAGE: pr-${{ github.event.number }}
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true

      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.AWS_DEPLOY_ROLE_ARN }}
          aws-region: us-east-1

      - run: go install github.com/sst-go/forge/cmd/forge@latest

      - name: Bootstrap state bucket
        run: forge bootstrap --stage $STAGE
        env:
          PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}

      - name: Deploy preview
        run: forge deploy --stage $STAGE 2>&1 | tee deploy-output.txt
        env:
          PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}

      - name: Comment on PR
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const output = fs.readFileSync('deploy-output.txt', 'utf8');
            const stage = process.env.STAGE;
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `### Preview deployed — stage \`${stage}\`\n\`\`\`\n${output.slice(-3000)}\n\`\`\``
            });
        env:
          STAGE: ${{ env.STAGE }}
```

### Tear down on PR close

```yaml
# .github/workflows/preview-cleanup.yml
name: Preview cleanup

on:
  pull_request:
    types: [closed]

permissions:
  id-token: write
  contents: read

jobs:
  teardown:
    runs-on: ubuntu-latest
    env:
      STAGE: pr-${{ github.event.number }}
    steps:
      - uses: actions/checkout@v4

      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache: true

      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.AWS_DEPLOY_ROLE_ARN }}
          aws-region: us-east-1

      - run: go install github.com/sst-go/forge/cmd/forge@latest

      - name: Tear down preview
        run: forge remove --stage $STAGE
        env:
          PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}
```

---

## Managing secrets in CI

### Setting secrets for a stage

Use `forge secret set` in a workflow step. Store the raw values in GitHub Secrets and forward them to SSM:

```yaml
- name: Sync prod secrets
  run: |
    forge secret set DatabaseURL "$DATABASE_URL" --stage prod
    forge secret set StripeApiKey "$STRIPE_API_KEY" --stage prod
  env:
    PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}
    DATABASE_URL: ${{ secrets.PROD_DATABASE_URL }}
    STRIPE_API_KEY: ${{ secrets.PROD_STRIPE_API_KEY }}
```

Run this step before `forge deploy` so secrets exist when Pulumi resolves them.

### Rotating a secret without a full redeploy

```yaml
- name: Rotate DB password
  run: forge secret set DatabaseURL "$NEW_DATABASE_URL" --stage prod
  env:
    NEW_DATABASE_URL: ${{ secrets.PROD_DATABASE_URL }}
    PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}
```

Then re-deploy to let Pulumi pick up the new value and update Lambda environment variables.

---

## Multiple AWS accounts

When staging and prod live in separate AWS accounts, use a different role ARN per environment. Store them as separate GitHub variables:

```
AWS_STAGING_ROLE_ARN  = arn:aws:iam::111111111111:role/forge-deploy
AWS_PROD_ROLE_ARN     = arn:aws:iam::222222222222:role/forge-deploy
```

```yaml
  deploy-staging:
    steps:
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.AWS_STAGING_ROLE_ARN }}
          aws-region: us-east-1
      - run: forge deploy --stage staging
        env:
          PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}

  deploy-prod:
    steps:
      - uses: aws-actions/configure-aws-credentials@v4
        with:
          role-to-assume: ${{ vars.AWS_PROD_ROLE_ARN }}
          aws-region: us-east-1
      - run: forge deploy --stage prod
        env:
          PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}
```

If you use `AWSProfile` in `StageConfig`, do not set it in CI — the OIDC-assumed role already provides credentials. Leave `AWSProfile` empty and let the ambient credentials take effect.

---

## Cloudflare deployments

For stacks that deploy Cloudflare Workers (or other CF resources), pass the Cloudflare API token as an environment variable:

```yaml
- name: Deploy
  run: forge deploy --stage prod
  env:
    PULUMI_CONFIG_PASSPHRASE: ${{ secrets.PULUMI_CONFIG_PASSPHRASE }}
    CLOUDFLARE_API_TOKEN: ${{ secrets.CLOUDFLARE_API_TOKEN }}
    CLOUDFLARE_ACCOUNT_ID: ${{ vars.CLOUDFLARE_ACCOUNT_ID }}
```

Store `CLOUDFLARE_API_TOKEN` as a GitHub Secret and `CLOUDFLARE_ACCOUNT_ID` as a variable (it's not sensitive).

---

## Caching Go builds

The `actions/setup-go` cache covers the Go module cache automatically when `cache: true` is set. For faster rebuilds on large projects, also cache the Go build cache:

```yaml
- uses: actions/setup-go@v5
  with:
    go-version: '1.24'
    cache: true                    # caches $GOPATH/pkg/mod

- name: Cache Go build cache
  uses: actions/cache@v4
  with:
    path: ~/.cache/go-build
    key: ${{ runner.os }}-go-build-${{ hashFiles('**/go.sum') }}
    restore-keys: ${{ runner.os }}-go-build-
```

---

## Full example: trunk-based with previews

A complete setup combining all patterns above into three workflow files:

| File | Trigger | What it does |
|---|---|---|
| `.github/workflows/preview.yml` | PR opened / updated | Deploy `pr-<N>` stage, comment outputs |
| `.github/workflows/preview-cleanup.yml` | PR closed | `forge remove --stage pr-<N>` |
| `.github/workflows/deploy.yml` | Push to `main` / git tag | Deploy `staging` (main) or `prod` (tag) |

This means every PR gets a live, isolated environment. Merging to main auto-deploys staging. Cutting a `v*` tag (with optional manual approval) deploys prod.

---

## Environment variable reference

| Variable | Where to store | Notes |
|---|---|---|
| `PULUMI_CONFIG_PASSPHRASE` | GitHub Secret | Required for Pulumi state encryption |
| `AWS_DEPLOY_ROLE_ARN` | GitHub Variable | OIDC role ARN — not sensitive |
| `CLOUDFLARE_API_TOKEN` | GitHub Secret | Required for Cloudflare constructs |
| `CLOUDFLARE_ACCOUNT_ID` | GitHub Variable | Required for Cloudflare constructs |
| `FORGE_STATE_BUCKET` | GitHub Variable | Optional — override S3 state bucket name |
| `AWS_DEFAULT_REGION` | Workflow env | Optional — overrides region from StageConfig |
