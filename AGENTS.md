# Agrofie — agent entrypoint

**End goal:** a Ghanaian talent marketplace (spec spelling *Agorofie*) connecting verified musicians/performers with event organizers. Discovery → booking → contract → **application-level fund hold** → completion → review. Admin mediates verification and disputes.

**Scaffold + auth:** structure, lookups, health, role shells, and **Ory Kratos–backed auth** (register/login/session/`me`). Not full MVP booking/payments yet.

## Repo map

| Path | Role |
| --- | --- |
| `agrofie-client/` | Expo SDK 57 (iOS / Android / web), TypeScript, Expo Router |
| `agrofie-backend/` | Go 1.26 modular services: GraphQL gateway + auth / booking / payments + Kratos |
| `docs/` | Canonical product & architecture docs (copied into backend too) |

## Do

- Read `docs/PRODUCT.md`, `docs/SCHEMA.md`, `docs/ARCHITECTURE.md` before coding domain features.
- Keep categories (genre, region, status, type) as **lookup tables** with immutable `code` + mutable `name`.
- Snapshot money/terms on bookings; do not rewrite history when taxonomy renames.
- Use `npx expo install` for Expo packages; `govulncheck` / `npm audit` before shipping deps.
- Identity credentials/sessions via **Kratos**; marketplace roles via `user_roles` + `roles` lookup.

## Don't

- Do not reuse the old Agermax Vuexy/Mongo/Stripe stack.
- Do not use Postgres ENUMs for statuses/categories.
- Do not treat Paystack Split as escrow (Paystack GH lists escrow as ineligible). Use platform hold + `escrow_ledger` + Transfer.
- Do not hardcode Accra/Highlife lists in the client — load from GraphQL lookups.
- Do not hand-roll JWT/session crypto — use Kratos.
- Do not commit secrets; use `.env.example` only.
- Do not allow public self-registration as `admin` — use `make seed-admin` / `go run ./cmd/seed-admin`.

## Run locally

```bash
# Backend
cd agrofie-backend
docker compose up --build

# Client (separate terminal)
cd agrofie-client
cp .env.example .env
npm run web
```

- GraphQL playground: http://localhost:8080/playground  
- Gateway GraphQL: http://localhost:8080/graphql  
- MailHog: http://localhost:8025  
- Spec source: https://docs.google.com/document/d/1wSHOyVUSD6VyVjcUyLbr7lxt0r1nbOZMHNBrvPtyXtA/edit

## Out of scope for now

Kubernetes/DOKS, live Paystack keys, SMS OTP, Ghana Card KYC, media CDN pipeline, real booking state machines, client talking to Kratos directly.
