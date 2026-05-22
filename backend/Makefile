.PHONY: build run test test-integration lint fmt tidy sqlc migrate-up migrate-down migrate-create

POSTGRES_URL ?= postgres://dora:dora@localhost:5432/dora?sslmode=disable

build:
	go build -o bin/api ./cmd/api
	go build -o bin/worker ./cmd/worker
	go build -o bin/mcp-server ./cmd/mcp-server

run:
	go run ./cmd/api

test:
	go test -race -cover ./...

# Integration tests via Testcontainers — sobe Postgres 18 real.
# Requer Docker disponível ao host onde o teste roda.
test-integration:
	go test -tags=integration -timeout=5m ./internal/api/...

lint:
	golangci-lint run ./...

fmt:
	go fmt ./...
	@command -v goimports >/dev/null 2>&1 && goimports -w . || true

tidy:
	go mod tidy

# sqlc — gera código de queries a partir de internal/storage/sql/queries/*.sql
sqlc:
	sqlc generate

# golang-migrate — assume binário 'migrate' instalado
migrate-up:
	migrate -path migrations -database "$(POSTGRES_URL)" up

migrate-down:
	migrate -path migrations -database "$(POSTGRES_URL)" down 1

migrate-create:
	@read -p "Migration name (snake_case): " name; \
	migrate create -ext sql -dir migrations -seq $$name
