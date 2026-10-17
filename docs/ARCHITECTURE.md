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
- **Split at payment time**: `POST /transaction/initialize` with `subaccount`, a flat `transaction_charge` (Agrofie's commission, in pesewas) and `bearer: "subaccount"` (decided: the talent bears Paystack's processing fee; `account` would make Agrofie absorb it). Net: Agrofie receives exactly `transaction_charge` (COMMISSION_PERCENT of the amount, rounded to the pesewa); the talent receives amount - commission - Paystack fee. Paystack returns 400 if the talent share cannot cover the fee. Checkout also sends `callback_url` (`<PUBLIC_BASE_URL>/pay/return`); webhooks verify amount/currency against the stored payment.
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

## Media uploads

`internal/media` owns uploads behind a `Storage` interface (Put/Open/Stat/Delete). `LocalDisk` (rooted at `MEDIA_DIR`, compose volume `media:/data/media`) is the only implementation; Cloudflare R2 / S3 is a later drop-in. Storage keys are server-generated (`<uuid>/<variant>`), never user filenames.

- Lookup `media_kinds` (`profile_photo`, `cover_image`, `verification_document`, `gallery_image`; immutable `code`, mutable `name`, `default_visibility`). Table `media` holds owner, kind, mime, size, dimensions, sha256, visibility (`public`/`private`, CHECK), status (`active`/`deleted`, CHECK). `talent_profiles.photo_media_id/cover_media_id`, `verifications.evidence_media_id` reference it.
- HTTP on the gateway: `POST /media` (multipart `file` + `kind`, Bearer session), `GET /media/{id}?v=thumb|medium`, `DELETE /media/{id}`. Images: sniffed by bytes (JPEG/PNG/WebP), EXIF auto-oriented then stripped by re-encoding to JPEG, variants original (<=2048px), medium 1024, thumb 256. HEIC is rejected (client converts to JPEG). PDFs only for `verification_document`. Limits: images 8 MB, PDFs 10 MB, 30 uploads per user per 10 min.
- Public kinds: no auth, `Cache-Control: public, max-age=31536000, immutable`. Private kinds: owner or admin via Bearer, or a short-lived HMAC-signed URL (`MEDIA_SIGNING_SECRET`, 10 min) returned by GraphQL for the admin queue.
- Orphans (uploaded, never attached) are removed by `make media-gc ARGS="-older-than 24h"`.
