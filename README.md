# agrofie-backend

Go 1.25 modular backend for **Agrofie** — Ghanaian talent marketplace.

## Services

| Service | Port | Role |
| --- | --- | --- |
| gateway | 8080 | GraphQL + playground |
| auth | 8081 | Identity (scaffold health) |
| booking | 8082 | Talent/bookings (scaffold health) |
| payments | 8083 | Escrow adapter stub |

## Quick start

```bash
cp .env.example .env
docker compose up --build
```

- Playground: http://localhost:8080/playground  
- Health: `GET /healthz` on each service  

## Make targets

```bash
make up        # docker compose
make migrate   # local migrate (needs DATABASE_URL)
make sqlc      # generate typed queries
make proto     # Buf Connect/gRPC codegen
make vuln      # govulncheck
make test
```

## Docs

Start with [AGENTS.md](./AGENTS.md) and [docs/](./docs/).

Schema migrations: `db/migrations/`. GraphQL schema: `graph/schema.graphqls`.
