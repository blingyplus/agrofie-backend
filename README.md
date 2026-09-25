# agrofie-backend

Go modular backend for **Agrofie** — Ghanaian talent marketplace.

## Services

| Service | Port | Role |
| --- | --- | --- |
| gateway | 8080 | GraphQL + playground + Bearer auth |
| auth | 8081 | Identity via Ory Kratos |
| booking | 8082 | Talent/bookings (scaffold health) |
| payments | 8083 | Escrow adapter stub |
| kratos | 4433/4434 | Identity (public/admin) |
| mailhog | 8025 | Dev SMTP UI |

## Quick start

```bash
cp .env.example .env
docker compose up --build
go run ./cmd/seed-admin   # optional local admin
```

- Playground: http://localhost:8080/playground  
- Health: `GET /healthz` on each app service  

## Make targets

```bash
make up          # docker compose
make migrate     # local migrate (needs DATABASE_URL)
make sqlc        # generate typed queries
make proto       # Buf Connect/gRPC codegen
make gqlgen      # GraphQL codegen
make seed-admin  # create admin from ADMIN_* env
make vuln        # govulncheck
make test
```

## Docs

Start with [AGENTS.md](./AGENTS.md) and [docs/](./docs/).

Schema migrations: `db/migrations/`. GraphQL schema: `graph/schema.graphqls`. Kratos config: `deploy/kratos/`.
