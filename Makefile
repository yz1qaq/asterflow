ASTERFLOW_GO := env -u GOROOT GOTOOLCHAIN=go1.26.6 go

.PHONY: docs-check deps-check test vet build run check

docs-check:
	python3 scripts/check_repo.py
	python3 -m unittest discover -s scripts -p 'test_*.py' -v
	git diff --check
	git diff --cached --check

deps-check:
	$(ASTERFLOW_GO) mod tidy -diff

test:
	GIN_MODE=test $(ASTERFLOW_GO) test -race -count=1 ./...

vet:
	$(ASTERFLOW_GO) vet ./...

build:
	mkdir -p bin
	$(ASTERFLOW_GO) build -mod=readonly -o bin/asterflow ./cmd/server
	$(ASTERFLOW_GO) build -mod=readonly -o bin/asterflow-migrate ./cmd/migrate

run:
	$(ASTERFLOW_GO) run ./cmd/server

check: docs-check deps-check test vet build web-check

# Local PostgreSQL only; existing project containers and volumes are untouched.
ASTERFLOW_COMPOSE := docker compose --env-file .env -f deploy/compose.yaml

.PHONY: db-up db-stop db-down db-status test-postgres

db-up:
	$(ASTERFLOW_COMPOSE) up -d --wait --wait-timeout 60 postgres

db-stop:
	$(ASTERFLOW_COMPOSE) stop postgres

db-down:
	$(ASTERFLOW_COMPOSE) down

db-status:
	$(ASTERFLOW_COMPOSE) ps

test-postgres:
	@test -n "$${ASTERFLOW_TEST_DATABASE_URL:-}" || { echo "Export ASTERFLOW_TEST_DATABASE_URL before running test-postgres"; exit 1; }
	$(ASTERFLOW_GO) test -race -count=1 -v ./internal/infrastructure/postgres

# Explicit migration commands; regular server startup does not change schema.
.PHONY: migrate-up migrate-version migrate-down test-migrations

migrate-up:
	$(ASTERFLOW_GO) run ./cmd/migrate -up

migrate-version:
	$(ASTERFLOW_GO) run ./cmd/migrate -version

migrate-down:
	$(ASTERFLOW_GO) run ./cmd/migrate -down 1

test-migrations:
	@test -n "$${ASTERFLOW_TEST_DATABASE_URL:-}" || { echo "Export ASTERFLOW_TEST_DATABASE_URL before running test-migrations"; exit 1; }
	$(ASTERFLOW_GO) test -race -count=1 -v ./cmd/migrate

# Pinned generator; source queries and schema stay under version control.
.PHONY: sqlc-generate

sqlc-generate:
	docker run --rm -v "$(CURDIR):/src" -w /src sqlc/sqlc:1.27.0 generate


.PHONY: web-install web-dev web-test web-build web-check

web-install:
	npm --prefix web/admin ci

web-dev:
	npm --prefix web/admin run dev -- --port "$${ASTERFLOW_WEB_PORT:-15174}"

web-test:
	npm --prefix web/admin test

web-build:
	npm --prefix web/admin run build

web-check: web-test web-build
