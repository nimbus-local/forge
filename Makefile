.PHONY: setup test build build-stub

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
