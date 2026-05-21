# Secrets

forge stores secrets in AWS Systems Manager (SSM) Parameter Store as `SecureString` values. Secrets are stage-isolated and injected into Lambda functions at deploy time.

---

## Storage path

```
/forge/<appName>/<stage>/<secretName>
```

Example: `/forge/myapp/prod/DatabaseURL`

---

## Managing secrets with the CLI

```bash
# Set a secret
forge secret set DatabaseURL "postgres://user:pass@host:5432/db"

# Get a secret (prints the value)
forge secret get DatabaseURL

# Remove a secret
forge secret remove DatabaseURL

# List all secrets for the current stage
forge secret list
```

Pass `--stage` to target a specific stage:

```bash
forge secret set DatabaseURL "..." --stage prod
forge secret list --stage staging
```

---

## Using secrets in infrastructure

Declare a secret in your `sst.config.go` and link it to functions that need it:

```go
dbURL    := constructs.NewSecret(ctx, "DatabaseURL", nil)
apiToken := constructs.NewSecret(ctx, "StripeApiKey", nil)

api := constructs.NewFunction(ctx, "Api", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{dbURL, apiToken},
})
```

If the secret has not been set yet, deployment fails with:

```
forge: secret "DatabaseURL" not found at /forge/myapp/prod/DatabaseURL
  → run: forge secret set DatabaseURL <value>
```

---

## Providing a default

```go
constructs.NewSecret(ctx, "FeatureFlag", &constructs.SecretArgs{
    Default: "false",
})
```

The `Default` value is stored in Pulumi state in plaintext. Only use it for non-sensitive values (flags, placeholders). Never put real secrets in `Default`.

---

## Reading secrets in Lambda handlers

Secrets are injected as `SST_SECRET_<NAME>` environment variables:

```go
dbURL    := os.Getenv("SST_SECRET_DATABASEURL")
apiToken := os.Getenv("SST_SECRET_STRIPEAPIKEY")
```

---

## Dev mode

In `forge dev`, all SSM secrets for the active stage are loaded and injected into the local process environment before running handlers. You do not need to call SSM from within your handler code.

---

## Rotating secrets

Update the value in SSM:

```bash
forge secret set DatabaseURL "postgres://user:newpass@host:5432/db" --stage prod
```

Then redeploy so Pulumi picks up the new value:

```bash
forge deploy --stage prod
```

---

## Security notes

- Parameters are stored as `SecureString` (KMS-encrypted) in SSM.
- The resolved value is injected at deploy time — it is also stored in Pulumi state as part of the stack snapshot. Treat your state bucket as sensitive.
- Never commit secret values in code. Use `forge secret set` only.
- Use per-stage secrets so prod credentials are never accessible in dev stages.
