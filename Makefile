.PHONY: payments-real payments-fake up down migrate proto sqlc gqlgen test vuln tidy seed-admin seed-talent seed-demo media-gc

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
	go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate

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

seed-talent:
	go run ./cmd/seed-talent

# Idempotent demo data for manual testing against `docker compose up`:
# admin + verified talents (profiles, rates, fake payout, availability) + organizer.
# Test credentials only; see .env.example / README.
seed-demo:
	ADMIN_PASSWORD=$${ADMIN_PASSWORD:-changeme-admin} go run ./cmd/seed-admin
	go run ./cmd/seed-talent

# Flip PAYMENTS_PROVIDER in .env and recreate gateway+payments. real = Paystack TEST API
# (needs PAYSTACK_SECRET_KEY=sk_test_... in .env); fake = no-money test page. See README.
payments-real:
	scripts/payments-mode.sh real

payments-fake:
	scripts/payments-mode.sh fake

run-gateway:
	go run ./cmd/gateway

run-auth:
	go run ./cmd/auth

run-booking:
	go run ./cmd/booking

run-payments:
	go run ./cmd/payments

# Remove uploads never attached to a profile/verification (default: older than 24h).
media-gc:
	go run ./cmd/media-gc $(ARGS)
