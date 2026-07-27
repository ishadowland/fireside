# Fireside Makefile
# Sprint 0 wiring targets. All targets are idempotent or destructive (clearly marked).
#
# Conventions:
#   - .env is loaded automatically by docker-compose and Go (env -file); do not commit it.
#   - DB target names mirror the RFC §Makefile targets section.
#   - Anything that boots the backend or migrates the DB requires `make db.up` first.

# ---- Config ----
ENV_FILE        ?= .env
DB_URL          ?= $(shell grep -E '^POSTGRES_DSN=' $(ENV_FILE) | cut -d= -f2-)
MIGRATE_CLI     ?= $(shell go env GOPATH)/bin/migrate
SQLC_CLI        ?= $(shell go env GOPATH)/bin/sqlc
GOLANGCI_LINT   ?= $(shell go env GOPATH)/bin/golangci-lint

# ---- DB ----
.PHONY: db.up
db.up: ## Start local Postgres (docker compose); wait until healthy.
	docker compose up -d postgres
	docker compose exec -T postgres pg_isready -U fireside -d fireside

.PHONY: db.down
db.down: ## Stop local Postgres and **drop the volume** (destructive).
	docker compose down -v

.PHONY: db.shell
db.shell: ## Open psql against the local DB.
	docker compose exec postgres psql -U fireside -d fireside

# ---- Migrations ----
.PHONY: migrate.up
migrate.up: ## Apply all up migrations.
	$(MIGRATE_CLI) -path db/migrations -database "$(DB_URL)" up

.PHONY: migrate.down
migrate.down: ## Roll back the most recent migration.
	$(MIGRATE_CLI) -path db/migrations -database "$(DB_URL)" down 1

# ---- sqlc ----
.PHONY: sqlc.install
sqlc.install: ## Install sqlc CLI (one-time).
	go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest

.PHONY: sqlc.generate
sqlc.generate: ## Regenerate internal/store from db/queries/*.sql.
	$(SQLC_CLI) generate

.PHONY: sqlc.verify
sqlc.verify: ## Verify generated code is in sync (use in CI).
	$(SQLC_CLI) vet

# ---- Backend (Go) ----
.PHONY: backend.tidy
backend.tidy: ## go mod tidy.
	go mod tidy

.PHONY: backend.run
backend.run: ## Run the backend with .env loaded.
	set -a && . ./$(ENV_FILE) && set +a && go run ./cmd/fireside

.PHONY: backend.test
backend.test: ## Run all Go tests.
	go test ./...

.PHONY: backend.lint
backend.lint: ## Run golangci-lint.
	$(GOLANGCI_LINT) run ./...

# ---- Android ----
.PHONY: android.install
android.install: ## Build the Android debug APK.
	cd android && ./gradlew assembleDebug

.PHONY: android.test
android.test: ## Run Android unit tests.
	cd android && ./gradlew test

# ---- Misc ----
.PHONY: help
help: ## Show this help text.
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_\.\-]+:.*?## / {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}' $(MAKEFILE_LIST)

.DEFAULT_GOAL := help