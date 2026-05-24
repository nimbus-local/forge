.PHONY: build deploy diff remove deploy-nimbus smoke-nimbus

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

# Deploy against the local Nimbus emulator (http://localhost:4566).
deploy-nimbus: build
	cd infra && FORGE_AWS_ENDPOINT=http://localhost:4566 PULUMI_CONFIG_PASSPHRASE='' forge deploy

# Deploy to Nimbus, then assert via the AWS CLI that:
#   1. The KMS key was created
#   2. The Handler Lambda has SST_KMS_SMOKE_KEY_ARN injected in its env
smoke-nimbus: deploy-nimbus
	@STAGE=$${FORGE_STAGE:-$${USER:-dev}}; \
	FN="forge-smoke-$${STAGE}-Handler"; \
	NIMBUS="aws --endpoint-url http://localhost:4566 --region us-east-1 --no-cli-pager"; \
	echo "── Asserting KMS key exists in Nimbus..."; \
	KEY_COUNT=$$($$NIMBUS kms list-keys --query 'length(Keys)' --output text); \
	if [ "$$KEY_COUNT" -lt 1 ]; then echo "ERROR: no KMS keys found" && exit 1; fi; \
	echo "   KMS keys: $$KEY_COUNT ✓"; \
	echo "── Asserting SST_KMS_SMOKE_KEY_ARN is in Lambda env ($$FN)..."; \
	ENV_VARS=$$($$NIMBUS lambda get-function-configuration --function-name "$$FN" --query 'Environment.Variables' --output json 2>/dev/null); \
	if [ -z "$$ENV_VARS" ] || [ "$$ENV_VARS" = "null" ]; then echo "ERROR: could not read Lambda env vars for $$FN" && exit 1; fi; \
	echo "   Env vars: $$ENV_VARS"; \
	echo "$$ENV_VARS" | python3 -c "import sys, json; v=json.load(sys.stdin); k='SST_KMS_SMOKE_KEY_ARN'; print(f'   {k} =', v.get(k,'MISSING')); sys.exit(0 if k in v else 1)"; \
	echo "── Smoke: KMS integration verified ✓"
