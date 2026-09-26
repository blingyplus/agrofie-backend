# Local development

## Prerequisites

- Node.js **22.13+** (Expo SDK 57)
- Go **1.26+**
- Docker Desktop

## Backend

```bash
cd agrofie-backend
cp .env.example .env
docker compose up --build
```

Compose brings up Postgres, Redis, MailHog, Kratos (migrate + serve), auth, booking, payments, gateway.

Smoke checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
curl http://localhost:8082/healthz
curl http://localhost:8083/healthz
curl -s http://localhost:8080/graphql -H "Content-Type: application/json" -d "{\"query\":\"{ health { status auth booking payments } }\"}"
```

Auth smoke (register talent):

```bash
curl -s http://localhost:8080/graphql -H "Content-Type: application/json" -d "{\"query\":\"mutation { register(input: { email: \\\"talent@example.com\\\", password: \\\"password12\\\", displayName: \\\"Test Talent\\\", roleCode: \\\"talent\\\" }) { sessionToken user { id roleCodes } } }\"}"
```

Seed a local admin (after Compose is up):

```bash
# From agrofie-backend with .env loaded (ADMIN_EMAIL / ADMIN_PASSWORD)
go run ./cmd/seed-admin
```

| URL | What |
| --- | --- |
| http://localhost:8080/playground | GraphQL playground |
| http://localhost:8080/graphql | Gateway GraphQL |
| http://localhost:4433 | Kratos public (internal — clients use gateway) |
| http://localhost:8025 | MailHog UI |

Without Docker (Postgres+Redis+Kratos already running):

```bash
make migrate
make run-auth      # :8081  (needs KRATOS_* env)
make run-booking   # :8082
make run-payments  # :8083
make run-gateway   # :8080
```

Fresh DB after schema changes: `docker compose down -v` then `up --build` (Postgres init + Kratos schema).

## Client

```bash
cd agrofie-client
cp .env.example .env
npm install
npm run web          # serves on :8090 (8081 is the auth service); or npm start / android / ios
```

Set `EXPO_PUBLIC_API_URL` if the gateway is not on `http://localhost:8080`.

On a physical device, use your machine LAN IP instead of `localhost`.

## Demo data and tests

```bash
cd agrofie-backend
# Demo talent (registers 12 acts through Kratos; safe to re-run). Needs the stack running.
export SEED_TALENT_PASSWORD=...   # see .env.example
make seed-talent

# Postgres-backed tests roll back their transaction; they skip when DATABASE_URL is unset.
DATABASE_URL='postgres://agrofie:agrofie@localhost:5432/agrofie?sslmode=disable' go test ./...
```

Host ports used by Compose: postgres 5432, redis **6380** (6379 is often a local Redis), mailhog 1025/8025, kratos 4433/4434, auth 8081, booking 8082, payments 8083, gateway 8080.

If Expo web shows stale code after an edit (the file watcher can miss changes), restart it with `npx expo start --web --port 8090 --clear`.

## Codegen

```bash
# From agrofie-client — reads ../agrofie-backend/graph/schema.graphqls
npm run codegen
```

GraphQL documents live in `agrofie-client/src/lib/queries.ts` (tagged `/* GraphQL */`); codegen reads them from there.

## Tear down

```bash
cd agrofie-backend
docker compose down -v
```
