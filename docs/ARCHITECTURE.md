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
| `cmd/payments` | 8083 | Paystack adapter: subaccounts, transaction init, signed webhooks, verify (stub today; Connect + `/healthz`) |
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
| `internal/payments` | Payment provider port. Paystack adapter for real use, fake adapter for tests. |
| `internal/httpsvc` | HTTP/Connect adapters for auth/booking/payments |
| `internal/gateway/probe` | Connect client that probes downstream health |
| `graph/` | gqlgen schema, generated exec, resolvers, auth directives |
| `graph/gqlauth` | Bearer middleware + principal context |

Generate: `make sqlc`, `make gqlgen`, `make proto`.

## API protocols

- **External:** GraphQL via gqlgen (clients request only needed fields — important for bandwidth).
- **Internal:** ConnectRPC contracts in `proto/agrofie/v1` (`make proto`). Services expose Connect Health + `/healthz`. Gateway probes via Connect clients.

## Payments (Ghana): Paystack split, no held funds

Agrofie never holds client funds. See `PRODUCT.md` (Money model). Paystack facts, checked against Paystack's docs and the account dashboard:

- **Subaccount per talent** (`POST /subaccount`): business name, settlement account, GHS. The dashboard offers **Bank** and **Mobile Money** types. Paystack verifies the account number.
- **Split at payment time**: `POST /transaction/initialize` with `subaccount`, a flat `transaction_charge` (Agrofie's commission, in pesewas) and `bearer: "subaccount"` (decided: the talent bears Paystack's processing fee; `account` would make Agrofie absorb it).
- **Settlement**: Paystack settles each party to its own account on the normal cycle. Not delayed on purpose.
- **Payment channels** (enabled on the account): card, mobile money, bank transfer.
- **Webhooks**: verify `x-paystack-signature` (HMAC-SHA512 of the raw body with the secret key) **before** trusting the event. Return 200 fast; Paystack retries failures (live: every 3 min for 4 attempts, then hourly for 72 h). Process idempotently by event/reference.
- **Reconcile**: if `charge.success` has not arrived within 180 s for a mobile money charge, call Verify Transaction.
- **Amounts** are integers in pesewas. Never trust the client for payment status; only a verified webhook or Verify Transaction moves a booking to `paid`.
- **Test mode first**: build and test with Paystack test keys and the fake provider. Live keys only after launch review.

Interface: `internal/payments.Provider`; `FakeProvider` for tests; a Paystack adapter is the real one. Keys come from env, never git.

## Data principles

- Lookups for anything renameable/admin-controlled (see `SCHEMA.md`).
- No Postgres ENUMs for domain categories.
- Money/terms snapshotted on the booking; payment records are append-only and keyed by Paystack reference.

## Production target (documented, not installed)

- DigitalOcean Kubernetes (DOKS)
- Managed Postgres + Redis
- CI builds container images, tests, rolling deploys

Local parity: Docker Compose (`docker compose up`).
