.PHONY: up down migrate proto sqlc gqlgen test vuln run-gateway tidy

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
	go run github.com/bufbuild/buf/cmd/buf@latest generate

sqlc:
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@latest generate

gqlgen:
	go run github.com/99designs/gqlgen generate

tidy:
	go mod tidy

test:
	go test ./...

vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

run-gateway:
	HTTP_PORT=8080 go run ./cmd/gateway

run-auth:
	HTTP_PORT=8081 go run ./cmd/auth

run-booking:
	HTTP_PORT=8082 go run ./cmd/booking

run-payments:
	HTTP_PORT=8083 go run ./cmd/payments
