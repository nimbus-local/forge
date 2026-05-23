.PHONY: build deploy diff remove

# Compile the Go Lambda handler for ARM64 Linux and zip it.
build:
	GOARCH=arm64 GOOS=linux CGO_ENABLED=0 go build -o bootstrap ./functions/handler
	zip -j functions/handler.zip bootstrap
	rm bootstrap

# Build then deploy all infrastructure.
deploy: build
	cd infra && forge deploy

diff:
	cd infra && forge diff

remove:
	cd infra && forge remove
