.PHONY: setup test build

# Wire up the committed git hooks. Run once after cloning.
setup:
	git config core.hooksPath .githooks

build:
	go build ./...

test:
	go test ./... -short
