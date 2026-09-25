.PHONY: up down migrate proto sqlc gqlgen test vuln tidy seed-admin

DATABASE_URL ?= postgres://agrofie:agrofie@localhost:5432/agrofie?sslmode=disable

up:
	docker compose up --build -d

down:
	docker compose down -v

migrate:
	go run ./cmd/migrate -direction up

migrate-down:
	go run ./cmd/migrate -direction down

proto:
	go run github.com/bufbuild/buf/cmd/buf@v1.50.0 generate

sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.29.0 generate

gqlgen:
	go run github.com/99designs/gqlgen generate

tidy:
	go mod tidy

test:
	go test ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

seed-admin:
	go run ./cmd/seed-admin

run-gateway:
	go run ./cmd/gateway

run-auth:
	go run ./cmd/auth

run-booking:
	go run ./cmd/booking

run-payments:
	go run ./cmd/payments
