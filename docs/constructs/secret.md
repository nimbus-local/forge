# Secret

Resolves an SSM SecureString at deploy time and injects the value into linked Lambda functions.

```go
import "github.com/nimbus-local/forge/constructs"

dbURL := constructs.NewSecret(ctx, "DatabaseURL", nil)
```

---

## SecretArgs

| Field | Type | Default | Description |
|---|---|---|---|
| `Default` | `string` | `""` | Fallback value when the SSM parameter does not exist. **WARNING:** stored in Pulumi state — only use for non-sensitive placeholder defaults. |

If no `Default` is set and the SSM parameter is missing, deployment panics with an actionable message:

```
forge: secret "DatabaseURL" not found at /forge/myapp/prod/DatabaseURL
  → run: forge secret set DatabaseURL <value>
```

---

## Methods

```go
func (s *Secret) Value() pulumi.StringOutput  // resolved secret value
```

---

## Linkable

| Env var | Value |
|---|---|
| `SST_SECRET_<NAME>` | Resolved SSM parameter value |

---

## Setting secrets

```bash
# Set a secret for a stage
forge secret set DatabaseURL "postgres://..."

# List all secrets for the current stage
forge secret list

# Remove a secret
forge secret remove DatabaseURL
```

Secrets are stored in SSM Parameter Store as `SecureString` at:
```
/forge/<appName>/<stage>/<secretName>
```

---

## Example — database credentials

```go
dbURL    := constructs.NewSecret(ctx, "DatabaseURL", nil)
apiToken := constructs.NewSecret(ctx, "ApiToken", &constructs.SecretArgs{
    Default: "dev-placeholder",  // only use for non-sensitive defaults
})

api := constructs.NewFunction(ctx, "Api", &constructs.FunctionArgs{
    Handler: "bootstrap",
    Link:    []forge.Linkable{dbURL, apiToken},
})
```

Inside the handler:

```go
dbURL    := os.Getenv("SST_SECRET_DATABASEURL")
apiToken := os.Getenv("SST_SECRET_APITOKEN")
```
