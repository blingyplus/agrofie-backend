# Architecture

## Shape

**Modular monorepo backend** (`agrofie-backend`): multiple Go binaries, shared module, one Postgres. Splits cleanly to Kubernetes services later.

```
Expo client  --GraphQL-->  gateway  --HTTP/ConnectRPC-->  auth | booking | payments
                                |                              |
                             Postgres 17                    Redis 8
```

| Binary | Port | Responsibility |
| --- | --- | --- |
| `cmd/gateway` | 8080 | Public GraphQL, CORS, aggregates health |
| `cmd/auth` | 8081 | Users, sessions, roles, verifications |
| `cmd/booking` | 8082 | Talent, availability, bookings, reviews/disputes |
| `cmd/payments` | 8083 | Escrow ledger + payment provider adapter |
| `cmd/migrate` | — | golang-migrate runner |

## API protocols

- **External:** GraphQL (clients request only needed fields — important for bandwidth).
- **Internal:** ConnectRPC / gRPC contracts in `proto/agrofie/v1` (`make proto`). Scaffold services expose `/healthz` over HTTP; wire generated Connect handlers as RPCs grow.

## Payments (Ghana)

Paystack **does not** offer true escrow products in Ghana (ineligible category). Agrofie uses:

1. Charge organizer on the **platform merchant** (card / MoMo GHS).
2. Append `escrow_ledger` row (`hold`).
3. On completion, **Transfer** to talent; ledger `release` (and `commission`).

Interface: `internal/payments.Provider` with `FakeProvider` for local scaffold. Never treat Paystack Split as escrow.

## Data principles

- Lookups for anything renameable/admin-controlled (see `SCHEMA.md`).
- No Postgres ENUMs for domain categories.
- Money/terms snapshotted on booking/ledger.

## Production target (documented, not installed)

- DigitalOcean Kubernetes (DOKS)
- Managed Postgres + Redis
- CI builds container images, tests, rolling deploys

Local parity: Docker Compose (`docker compose up`).
