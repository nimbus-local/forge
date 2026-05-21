# Dev Tunnel

`forge dev` lets you run Lambda handler code locally while using real AWS triggers — API Gateway routes, SQS messages, EventBridge events. Invocations are forwarded to your machine in real time.

---

## Architecture

```
AWS (stub Lambda)                     Local machine (forge dev)
─────────────────────────────────     ──────────────────────────────────
Real trigger (API GW, SQS, ...)  →   forge dev polls SQS request queue
  ↓                                    ↓
Stub Lambda receives invocation       Looks up registered handler binary
  ↓                                    ↓
Publishes to SQS request queue        Runs binary with event piped to stdin
  ↓                                    ↓
Long-polls SQS response queue    ←   Sends response to SQS response queue
  ↓
Returns response to caller
```

Stub Lambdas are thin Go binaries deployed alongside your real handlers. They do no business logic — they just proxy events over SQS.

### SQS queues

| Queue | Name |
|---|---|
| Request | `forge-<app>-<stage>-req` |
| Response | `forge-<app>-<stage>-res` |

---

## Starting dev mode

```bash
forge dev --stage dev
```

forge deploys the stub infrastructure (if not already deployed) and starts polling. Source-code changes are picked up automatically — the next invocation runs the latest binary.

---

## What happens on each invocation

1. A real AWS trigger fires (HTTP request, SQS message, cron tick).
2. The stub Lambda publishes the full invocation event + context to the request queue.
3. `forge dev` receives the event, finds the matching handler registered for that function ARN.
4. Runs the handler binary with the event JSON piped to stdin.
5. Reads the response from stdout and publishes it to the response queue.
6. The stub Lambda returns the response to the original caller.

The round-trip adds roughly 50–200 ms of latency depending on SQS poll interval. The stub has a 29-second timeout (Lambda max - 1s) for the response.

---

## Handler registration

Handlers are registered by function name. forge reads your `sst.config.go` to discover which local binaries correspond to which deployed function ARNs. No extra configuration is needed.

---

## Environment variables in dev mode

All SSM secrets for the active stage are loaded and injected into the local process before handlers run. Resource env vars (`SST_TABLE_*`, `SST_BUCKET_*`, etc.) point at the real deployed resources.

This means your local handler code reads from and writes to real AWS resources (DynamoDB, S3, SQS). Use a personal dev stage (e.g. `--stage alice`) to avoid affecting shared environments.

---

## Differences from production

| Aspect | Dev tunnel | Production Lambda |
|---|---|---|
| Cold starts | None (process is long-lived) | Occasional |
| Latency | +50–200 ms (SQS round-trip) | Native |
| Concurrency | Single-threaded | Parallel Lambda invocations |
| Logs | Written to your terminal | CloudWatch Logs |
| Crashes | Process restarts on next request | Lambda restarts automatically |

---

## Stopping dev mode

Press `Ctrl+C`. forge tears down the SQS polling loop. The stub Lambda and SQS queues remain deployed — they only forward events when `forge dev` is running.
