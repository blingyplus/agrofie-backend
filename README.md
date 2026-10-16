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

## Manual end-to-end testing (phone / no Paystack)

```bash
# 1. set your Mac's LAN IP so a phone can open fake checkout pages (.env is gitignored)
echo 'PUBLIC_BASE_URL=http://<LAN-IP>:8080' >> .env      # e.g. http://192.168.100.98:8080
echo 'PAYMENTS_PROVIDER=fake' >> .env                     # optional if a Paystack key is set
docker compose up --build -d
make seed-demo                                           # admin + 12 verified talents (4 featured, ~76 reviewed bookings) + 4 organizers
scripts/e2e.sh                                           # scripted full walk; must print E2E PASSED
```

- Point the app at `http://<LAN-IP>:8080/graphql` (gateway binds all interfaces, CORS `*`).
- Registration/login never require email verification (identities are created pre-verified); MailHog is not needed.
- Demo logins (test values): `admin@agrofie.local` / `changeme-admin`, `demo.organizer@agrofie.local` / `changeme-seed-organizer`, `seed.kofi-mensimah@agrofie.local` (and the other `seed.*` talents) / `changeme-seed-talent`.
- **Fake payments** (used when `PAYSTACK_SECRET_KEY` is unset or `PAYMENTS_PROVIDER=fake`): `initiateCheckout` returns `<PUBLIC_BASE_URL>/pay/fake/<reference>`, a labelled TEST page whose button sends a signed `charge.success` through the real webhook path (`agreed -> paid`). `verifyPayment` ("I've paid") also succeeds. No money moves. With real Paystack test keys, use Paystack's test card and then `verifyPayment` (webhooks can't reach localhost).
- `DEV_ALLOW_EARLY_COMPLETE=true` (compose default) lets a paid booking be completed before its event date so reviews can be tested. Turn off for real use.
- There is no counter-offer mutation; talents accept or decline.
