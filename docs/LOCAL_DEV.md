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

Smoke checks:

```bash
curl http://localhost:8080/healthz
curl http://localhost:8081/healthz
curl http://localhost:8082/healthz
curl http://localhost:8083/healthz
curl -s http://localhost:8080/graphql -H "Content-Type: application/json" -d "{\"query\":\"{ health { status auth booking payments } }\"}"
```

Playground: http://localhost:8080/playground

Without Docker (Postgres+Redis already running):

```bash
make migrate
make run-auth      # :8081
make run-booking   # :8082
make run-payments  # :8083
make run-gateway   # :8080
```

## Client

```bash
cd agrofie-client
cp .env.example .env
npm install
npm run web          # or npm start / android / ios
```

Set `EXPO_PUBLIC_API_URL` if the gateway is not on `http://localhost:8080`.

On a physical device, use your machine LAN IP instead of `localhost`.

## Codegen

```bash
# From agrofie-client — reads ../agrofie-backend/graph/schema.graphqls
npm run codegen
```

## Tear down

```bash
cd agrofie-backend
docker compose down -v
```
