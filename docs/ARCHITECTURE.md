# Architecture

## Shape

**Modular monorepo backend** (`agrofie-backend`): multiple Go binaries, shared module, one Postgres. Splits cleanly to Kubernetes services later.

```
Expo client  --GraphQL (gqlgen)-->  gateway  --ConnectRPC-->  auth | booking | payments
                                         |                    |
                                      sqlc/pgx              Redis 8
                                         |                    |
                                      Postgres 17 <------- Ory Kratos
                                         |                    |
                                      (public)           MailHog (dev SMTP)
```

| Binary | Port | Responsibility |
| --- | --- | --- |
| `cmd/gateway` | 8080 | Public GraphQL (gqlgen), CORS, Bearer → WhoAmI, Connect health probe |
| `cmd/auth` | 8081 | Identity adapter over Kratos (Connect Register/Login/Logout/WhoAmI) |
| `cmd/booking` | 8082 | Talent/bookings (Connect + `/healthz`) |
| `cmd/payments` | 8083 | Escrow adapter stub (Connect + `/healthz`) |
| `cmd/migrate` | — | golang-migrate runner |
| `cmd/seed-admin` | — | Local admin via Kratos + `users`/`user_roles` (env credentials) |

### Identity

- **Ory Kratos** owns credentials, password hashing, and opaque session tokens.
- Marketplace **`users` / `profiles` / `user_roles`** remain the source of truth for product FKs (`kratos_identity_id` links the two).
- Expo talks **only** to the GraphQL gateway — never to Kratos directly.
- Roles (`talent` / `organizer` / `admin`) stay in the `roles` lookup. Public register allows talent/organizer only.

### Package layout (hardened)

| Package | Role |
| --- | --- |
| `internal/domain` | Shared immutable codes (roles, booking statuses) |
| `internal/db` | sqlc-generated typed SQL |
| `internal/auth` | Kratos client + marketplace user provisioning |
| `internal/lookup` | Application service for admin-controlled taxonomies |
| `internal/payments` | Payment provider port + fake adapter |
| `internal/httpsvc` | HTTP/Connect adapters for auth/booking/payments |
| `internal/gateway/probe` | Connect client that probes downstream health |
| `graph/` | gqlgen schema, generated exec, resolvers, auth directives |
| `graph/gqlauth` | Bearer middleware + principal context |

Generate: `make sqlc`, `make gqlgen`, `make proto`.

## API protocols

- **External:** GraphQL via gqlgen (clients request only needed fields — important for bandwidth).
- **Internal:** ConnectRPC contracts in `proto/agrofie/v1` (`make proto`). Services expose Connect Health + `/healthz`. Gateway probes via Connect clients.

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
