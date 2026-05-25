.PHONY: setup test build build-stub smoke smoke-clean smoke-nuke

# Wire up the committed git hooks. Run once after cloning.
setup:
	git config core.hooksPath .githooks

# Cross-compile forge-stub for linux/amd64 (Lambda runtime). Used by `forge dev`.
# The resulting binary is built on demand at `forge dev` time via `go list -m`.
# Run this target to verify the stub compiles cleanly.
build-stub:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o /dev/null ./cmd/forge-stub/

build:
	go build ./...

test:
	go test ./... -short

# ── Local smoke test against Nimbus ──────────────────────────────────────────
# Requires Nimbus running at localhost:4566 (cd ~/source/nimbus-local/nimbus && docker compose up -d).
# Mirrors the steps in .github/workflows/smoke.yml.

SMOKE_ENV := \
	AWS_ACCESS_KEY_ID=test \
	AWS_SECRET_ACCESS_KEY=test \
	AWS_DEFAULT_REGION=us-east-1 \
	AWS_ENDPOINT_URL=http://localhost:4566 \
	FORGE_AWS_ENDPOINT=http://localhost:4566 \
	PULUMI_CONFIG_PASSPHRASE=""

smoke: build
	@echo "=== Building forge CLI ==="
	go build -o /tmp/forge-local ./cmd/forge

	@echo "=== Building smoke Lambda handler ==="
	cd examples/smoke && \
		GOARCH=arm64 GOOS=linux CGO_ENABLED=0 \
		go build -o bootstrap ./functions/handler && \
		zip -j functions/handler.zip bootstrap && \
		rm bootstrap

	@echo "=== Seeding AppSecret in SSM ==="
	$(SMOKE_ENV) aws ssm put-parameter \
		--name /forge/forge-smoke/ci/AppSecret \
		--value "smoke-default" \
		--type SecureString \
		--overwrite \
		--endpoint-url http://localhost:4566 --region us-east-1 --no-cli-pager

	@echo "=== Deploying smoke stack ==="
	cd examples/smoke && $(SMOKE_ENV) /tmp/forge-local deploy --stage ci

	@echo "=== Running smoke assertions ==="
	$(SMOKE_ENV) .github/scripts/smoke-assert.sh

# smoke-nuke removes all forge-smoke-* S3 buckets (assets + state) from Nimbus.
# Run this after Nimbus restarts to clear stale state before re-deploying.
smoke-nuke:
	$(SMOKE_ENV) bash scripts/smoke-nuke.sh forge-smoke

smoke-clean:
	@echo "=== Removing smoke stack ==="
	@go build -o /tmp/forge-local ./cmd/forge 2>/dev/null || true
	cd examples/smoke && $(SMOKE_ENV) /tmp/forge-local remove --stage ci 2>/dev/null || true
