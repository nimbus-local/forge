.PHONY: nimbus-up nimbus-down build deploy-nimbus deploy query query-nimbus remove-nimbus remove

STAGE        ?= dev
NIMBUS_PORT  ?= 4567
NIMBUS_IMAGE := ghcr.io/nimbus-local/nimbus:0.4.22

APP    := appsync-notes
PREFIX := $(APP)-$(STAGE)

NIMBUS_ENV := \
	AWS_ACCESS_KEY_ID=test \
	AWS_SECRET_ACCESS_KEY=test \
	AWS_DEFAULT_REGION=us-east-1 \
	AWS_ENDPOINT_URL=http://localhost:$(NIMBUS_PORT) \
	FORGE_AWS_ENDPOINT=http://localhost:$(NIMBUS_PORT) \
	PULUMI_CONFIG_PASSPHRASE=""

NIMBUS_CLI := $(NIMBUS_ENV) aws \
	--endpoint-url http://localhost:$(NIMBUS_PORT) \
	--region us-east-1 --no-cli-pager --output json

# ── Nimbus ────────────────────────────────────────────────────────────────────

nimbus-up:
	docker run -d --name appsync-nimbus \
		-p $(NIMBUS_PORT):4566 \
		-e AWS_DEFAULT_REGION=us-east-1 \
		$(NIMBUS_IMAGE)
	@echo -n "Waiting for Nimbus..."
	@until curl -sf http://localhost:$(NIMBUS_PORT)/_nimbus/health >/dev/null 2>&1; do \
		printf "."; sleep 1; done
	@echo " ready"

nimbus-down:
	docker rm -f appsync-nimbus 2>/dev/null || true

# ── Build ─────────────────────────────────────────────────────────────────────

build:
	GOARCH=arm64 GOOS=linux CGO_ENABLED=0 go build -o bootstrap ./functions/resolver
	zip -j functions/resolver.zip bootstrap
	rm bootstrap

# ── Nimbus deploy / remove ────────────────────────────────────────────────────

deploy-nimbus: build
	cd infra && $(NIMBUS_ENV) forge deploy --stage $(STAGE)

remove-nimbus:
	cd infra && $(NIMBUS_ENV) forge remove --stage $(STAGE)

# ── GraphQL round-trip test (Nimbus) ─────────────────────────────────────────
# Uses the path-based execution endpoint: /_appsync/{apiId}/graphql
# Nimbus v0.4.22+ emulates Lambda-backed resolvers and API_KEY auth.

query-nimbus:
	@API_ID=$$($(NIMBUS_CLI) appsync list-graphql-apis \
		--query "graphqlApis[?name=='$(PREFIX)-Graph'].apiId | [0]" \
		--output text) && \
	API_KEY=$$($(NIMBUS_CLI) appsync list-api-keys --api-id $$API_ID \
		--query "apiKeys[0].id" --output text) && \
	BASE_URL="http://localhost:$(NIMBUS_PORT)/_appsync/$$API_ID/graphql" && \
	echo "URL: $$BASE_URL" && \
	echo "Key: $$API_KEY" && \
	echo "" && \
	echo "── createNote ──────────────────────────────────────────────────" && \
	curl -sf -X POST "$$BASE_URL" \
		-H "x-api-key: $$API_KEY" \
		-H "Content-Type: application/json" \
		-d '{"query":"mutation { createNote(id: \"note-1\", content: \"Hello from forge AppSync\") { id content createdAt } }"}' \
		| jq . && \
	echo "" && \
	echo "── getNote ─────────────────────────────────────────────────────" && \
	curl -sf -X POST "$$BASE_URL" \
		-H "x-api-key: $$API_KEY" \
		-H "Content-Type: application/json" \
		-d '{"query":"query { getNote(id: \"note-1\") { id content createdAt } }"}' \
		| jq . && \
	echo "" && \
	echo "── listNotes ───────────────────────────────────────────────────" && \
	curl -sf -X POST "$$BASE_URL" \
		-H "x-api-key: $$API_KEY" \
		-H "Content-Type: application/json" \
		-d '{"query":"query { listNotes { id content createdAt } }"}' \
		| jq . && \
	echo "" && \
	echo "── deleteNote ──────────────────────────────────────────────────" && \
	curl -sf -X POST "$$BASE_URL" \
		-H "x-api-key: $$API_KEY" \
		-H "Content-Type: application/json" \
		-d '{"query":"mutation { deleteNote(id: \"note-1\") }"}' \
		| jq .

# ── Real AWS deploy / remove ──────────────────────────────────────────────────
# Uses your default AWS credentials and region. Set STAGE before deploying:
#   STAGE=prod make deploy

deploy: build
	cd infra && forge deploy --stage $(STAGE)

remove:
	cd infra && forge remove --stage $(STAGE)

# ── GraphQL round-trip test (real AWS) ───────────────────────────────────────
# Sends a createNote mutation, getNote and listNotes queries, then deleteNote.
# The API URL and key are looked up from AWS using the CLI after deploying.
# For Nimbus, use query-nimbus instead.

query:
	@API_ID=$$(aws appsync list-graphql-apis \
		--query "graphqlApis[?name=='$(PREFIX)-Graph'].apiId | [0]" \
		--output text) && \
	API_URL=$$(aws appsync get-graphql-api --api-id $$API_ID \
		--query "graphqlApi.uris.GRAPHQL" --output text) && \
	API_KEY=$$(aws appsync list-api-keys --api-id $$API_ID \
		--query "apiKeys[0].id" --output text) && \
	echo "URL: $$API_URL" && \
	echo "Key: $$API_KEY" && \
	echo "" && \
	echo "── createNote ──────────────────────────────────────────────────" && \
	curl -sf -X POST "$$API_URL" \
		-H "x-api-key: $$API_KEY" \
		-H "Content-Type: application/json" \
		-d '{"query":"mutation { createNote(id: \"note-1\", content: \"Hello from forge AppSync\") { id content createdAt } }"}' \
		| jq . && \
	echo "" && \
	echo "── getNote ─────────────────────────────────────────────────────" && \
	curl -sf -X POST "$$API_URL" \
		-H "x-api-key: $$API_KEY" \
		-H "Content-Type: application/json" \
		-d '{"query":"query { getNote(id: \"note-1\") { id content createdAt } }"}' \
		| jq . && \
	echo "" && \
	echo "── listNotes ───────────────────────────────────────────────────" && \
	curl -sf -X POST "$$API_URL" \
		-H "x-api-key: $$API_KEY" \
		-H "Content-Type: application/json" \
		-d '{"query":"query { listNotes { id content createdAt } }"}' \
		| jq . && \
	echo "" && \
	echo "── deleteNote ──────────────────────────────────────────────────" && \
	curl -sf -X POST "$$API_URL" \
		-H "x-api-key: $$API_KEY" \
		-H "Content-Type: application/json" \
		-d '{"query":"mutation { deleteNote(id: \"note-1\") }"}' \
		| jq .
