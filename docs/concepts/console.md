# Web Console

`forge console` starts a local HTTP server and opens a browser-based dashboard for the active stage. It shows stack outputs, deployed resources, and secrets — with live data fetched on load and on each Refresh click.

---

## Usage

```bash
forge console                    # uses $USER or "dev" as the stage
forge console --stage prod       # inspect the prod stage
forge console --stage prod --port 8080   # custom port (default: 3000)
```

The console opens automatically at `http://localhost:3000`.

---

## What it shows

### Outputs tab

Stack outputs exported via `ctx.Export()` in your config. URL values are rendered as clickable links. Each value has a **Copy** button.

```go
ctx.Export("url", api.URL())
ctx.Export("bucket", bucket.Name())
```

Secret outputs (Pulumi marked secrets) are shown as `••••••` and cannot be copied.

### Resources tab

Every resource in the Pulumi stack state, filtered to remove internal Pulumi bookkeeping resources. Shows the resource type, logical name, and AWS physical ID.

### Secrets tab

All secrets stored in SSM Parameter Store for this app/stage (`/forge/<app>/<stage>/*`). Only names are displayed — values are never fetched.

---

## Requirements

- The stage must have been deployed at least once (`forge deploy`) before the console can connect to its stack state.
- AWS credentials must be available in the environment (same as `forge deploy`).
- `PULUMI_CONFIG_PASSPHRASE` must be set if your stack uses state encryption.

---

## App name resolution

The console reads the app name from `sst.config.go` (looks for `Name: "..."` in the file). If your config sets the name dynamically, set the `FORGE_APP` env var instead:

```bash
FORGE_APP=my-app forge console --stage prod
```

---

## Flags

| Flag | Default | Description |
|---|---|---|
| `--stage`, `-s` | `$USER` / `dev` | Stage to inspect |
| `--port`, `-p` | `3000` | Local port for the console server |
| `--profile` | — | AWS credentials profile |
| `--region` | — | AWS region override |
