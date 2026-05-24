# Contributing to forge

## Prerequisites

- Go 1.22+
- An AWS account (for integration/e2e tests only — unit tests run offline)

## Setup

```bash
git clone git@github.com:nimbus-local/forge.git
cd forge
make setup   # installs the pre-commit hook via core.hooksPath = .githooks
```

The pre-commit hook runs on every commit and will:
- Block direct commits to `main` or `master`
- Run `go fmt ./...`
- Run `go build ./...`
- Run `go vet ./...`
- Run `go test ./... -short`

## Build and test commands

```bash
# Build the CLI
go build ./cmd/forge

# Run unit tests (no AWS required)
go test ./... -short

# Run all tests including integration (real AWS)
go test ./... -tags integration

# Run e2e tests against the compiled binary
go test ./test/e2e/... -tags e2e

# Test coverage
go test ./... -coverprofile=coverage.out && go tool cover -html=coverage.out

# Install CLI locally
go install ./cmd/forge
```

## Branch and PR workflow

- Always work on a branch — direct commits to `master` are blocked by the pre-commit hook
- Open a PR for every change, no matter how small
- Merge with `gh pr merge --merge` (no squash — preserves commit history)

## PR checklist

Complete all of these before opening a PR:

1. `go fmt ./...` passes (no diff)
2. `go build ./...` passes
3. `go test ./... -short` passes
4. New constructs have a `docs/constructs/<name>.md` doc page
5. `README.md` constructs table updated
6. `README.md` roadmap updated — check off completed items
7. `docs/README.md` concepts/constructs table updated
8. SST v3 → forge mapping table in `CLAUDE.md` updated
9. `constructs/` file structure comment in `CLAUDE.md` updated
