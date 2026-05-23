.PHONY: deploy remove dev

STAGE ?= dev

# Bootstrap state bucket (idempotent) then deploy.
deploy:
	cd infra && go mod tidy && forge bootstrap --stage $(STAGE) && forge deploy --stage $(STAGE)

remove:
	cd infra && forge remove --stage $(STAGE)

# Run the Next.js dev server locally.
dev:
	cd web && npm install && npm run dev
