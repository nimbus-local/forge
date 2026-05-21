.PHONY: build deploy remove

# Compile the Go Lambda for ARM64 Linux and zip it.
# The resulting functions/api.zip is referenced by the infra config.
build:
	GOARCH=arm64 GOOS=linux CGO_ENABLED=0 go build -o bootstrap ./functions/api
	zip -j functions/api.zip bootstrap
	rm bootstrap

# Build the Lambda binary then deploy all infrastructure.
deploy: build
	cd infra && forge deploy

remove:
	cd infra && forge remove
