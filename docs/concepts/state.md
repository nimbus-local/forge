# State

forge uses Pulumi's Automation API with an S3 backend to store infrastructure state. State tracks which AWS resources belong to which stack so Pulumi can compute diffs and clean up on removal.

---

## State bucket

Each stage gets its own state bucket:

```
s3://<appName>-<stage>-forge-state
```

The bucket is created automatically by `forge bootstrap` and by the first `forge deploy` if it doesn't exist yet.

### Bucket configuration

| Setting | Value |
|---|---|
| Versioning | Enabled |
| Encryption | SSE-S3 |
| Public access | Blocked |
| Lifecycle | Expire old state versions after 90 days |

---

## Bootstrap

Create the state bucket before your first deploy:

```bash
forge bootstrap              # creates bucket for the default (dev) stage
forge bootstrap --stage prod
```

`forge deploy` calls `forge bootstrap` automatically, so you rarely need to run it directly.

---

## Overriding the state bucket

Set `FORGE_STATE_BUCKET` to use a custom bucket name:

```bash
export FORGE_STATE_BUCKET=my-custom-state-bucket
forge deploy --stage prod
```

This is also how you point forge at an existing SST v3 Ion state bucket — the Pulumi S3 backend format is identical.

---

## Importing from SST v3 Ion

If you are migrating from SST Ion:

1. Find your Ion state bucket (usually `<app>-<stage>-ion-state` or similar).
2. Set `FORGE_STATE_BUCKET` to that bucket name.
3. Run `forge diff --stage <stage>` to verify forge can read the existing state.
4. Deploy with `forge deploy` — Pulumi will see the existing resources and skip re-creation.

---

## Encryption passphrase

Pulumi encrypts secret values in state using a passphrase:

```bash
export PULUMI_CONFIG_PASSPHRASE="your-passphrase"
forge deploy --stage prod
```

Set the same passphrase in CI and any other environment that reads the state bucket. If you lose the passphrase, secret outputs in state become unreadable (infrastructure still exists; only state values are affected).

---

## State file location

Within the bucket, state is stored at:

```
s3://<bucket>/.pulumi/stacks/<appName>/<stage>.json
```

The file is a Pulumi stack state snapshot — the same format Pulumi Cloud uses internally.

---

## Disaster recovery

Because versioning is enabled, you can restore a previous state version from the S3 console or CLI if a bad deploy corrupts the state file.

```bash
# List state versions
aws s3api list-object-versions \
  --bucket myapp-prod-forge-state \
  --prefix ".pulumi/stacks/myapp/prod.json"

# Restore a previous version (copy it over the current)
aws s3 cp \
  "s3://myapp-prod-forge-state/.pulumi/stacks/myapp/prod.json?versionId=<old-id>" \
  "s3://myapp-prod-forge-state/.pulumi/stacks/myapp/prod.json"
```
