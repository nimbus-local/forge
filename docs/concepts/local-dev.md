# Local Development with Nimbus

forge supports redirecting all AWS API calls to a local AWS emulator via the `FORGE_AWS_ENDPOINT` environment variable. The recommended emulator is **[Nimbus](https://github.com/nimbus-local/nimbus)** — a free, MIT-licensed drop-in replacement for LocalStack Community Edition that runs S3, SQS, DynamoDB, SSM Parameter Store, Lambda, API Gateway, and more on port `4566`.

---

## Quick start

**1. Start Nimbus:**

```bash
docker run -p 4566:4566 ghcr.io/nimbus-local/nimbus:latest
```

Or with Docker Compose (add DynamoDB Local for full DynamoDB support):

```yaml
services:
  nimbus:
    image: ghcr.io/nimbus-local/nimbus:latest
    ports:
      - "4566:4566"
    environment:
      AWS_DEFAULT_REGION: us-east-1

  dynamodb-local:
    image: amazon/dynamodb-local:latest
    command: "-jar DynamoDBLocal.jar -sharedDb -dbPath /data"
    volumes:
      - dynamodb_data:/data

volumes:
  dynamodb_data:
```

**2. Point forge at Nimbus:**

```bash
export FORGE_AWS_ENDPOINT=http://localhost:4566
forge deploy --stage local
```

That's it. forge automatically:
- Redirects all AWS SDK calls (S3, SQS, SSM) to `http://localhost:4566`
- Injects `AWS_ACCESS_KEY_ID=test` / `AWS_SECRET_ACCESS_KEY=test` if no credentials are configured — Nimbus accepts any value
- Enables S3 path-style addressing (required for `localhost` endpoints)
- Propagates the endpoint into the Pulumi workspace so the AWS provider and state backend also use Nimbus

---

## What gets redirected

| forge component | AWS service | Redirected |
|---|---|---|
| State bucket (bootstrap) | S3 | ✅ |
| `forge secret set/get/list` | SSM Parameter Store | ✅ |
| `forge dev` tunnel queues | SQS | ✅ |
| Pulumi AWS provider (all constructs) | All | ✅ via `AWS_ENDPOINT_URL` |
| Pulumi S3 state backend | S3 | ✅ via `AWS_ENDPOINT_URL` |

The forge-stub Lambda binary (deployed to real AWS during `forge dev`) always uses real SQS — it runs in Lambda and is unaffected by `FORGE_AWS_ENDPOINT`.

---

## Services supported by Nimbus

| Service | Used by forge for |
|---|---|
| S3 | Pulumi state bucket, `Bucket` construct |
| SQS | `forge dev` tunnel, `Queue` construct |
| SSM Parameter Store | `forge secret` commands, `Secret` construct |
| DynamoDB | `DynamoDB` construct (via DynamoDB Local sidecar) |
| Lambda | `Function` construct |
| API Gateway | `ApiGatewayV2` construct |
| EventBridge | `Cron` construct |
| SNS | `Topic` construct |

---

## Keeping credentials out of the way

If you have real AWS credentials configured (`~/.aws/credentials` or `AWS_ACCESS_KEY_ID` in env), forge uses them as-is — Nimbus ignores whatever credentials it receives.

If no credentials are configured, forge injects `AWS_ACCESS_KEY_ID=test` and `AWS_SECRET_ACCESS_KEY=test` automatically when `FORGE_AWS_ENDPOINT` is set, so the AWS SDK doesn't reject requests before they reach Nimbus.

---

## Limitations

- **Pulumi S3 backend path-style**: forge sets `AWS_S3_USE_PATH_STYLE=true` to ensure Pulumi's S3 state backend uses `http://localhost:4566/<bucket>` URLs rather than `http://<bucket>.localhost:4566`. Nimbus supports both, but path-style is more reliable on `localhost`.
- **Lambda execution**: Nimbus's Lambda implementation covers the management API (create/update/invoke). For `forge dev`, the tunnel still runs handlers locally — Lambda inside Nimbus is used for construct deployment only.
- **Cloudflare constructs**: `FORGE_AWS_ENDPOINT` has no effect on Cloudflare constructs — those call the Cloudflare API directly.

---

## Example: full local workflow

```bash
# Terminal 1 — start Nimbus
docker compose up

# Terminal 2 — deploy to local
export FORGE_AWS_ENDPOINT=http://localhost:4566
export FORGE_STATE_BUCKET=my-app-local-forge-state   # optional: fixed bucket name

forge bootstrap --stage local        # creates state bucket in Nimbus
forge deploy --stage local           # deploys all constructs to Nimbus
forge secret set DB_URL postgres://localhost/dev --stage local
forge diff --stage local             # preview changes

# Tear down
forge remove --stage local
```

---

## See also

- [Dev Tunnel](dev-tunnel.md) — running Lambda handlers locally with real AWS triggers
- [State](state.md) — S3 Pulumi state backend
- [Secrets](secrets.md) — SSM-backed secrets management
- [Nimbus repository](https://github.com/nimbus-local/nimbus)
