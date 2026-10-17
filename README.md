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

**Forgot password:** Kratos recovery (code method) via the gateway; locally the 6-digit code lands in MailHog (http://localhost:8025). **Production requires SMTP credentials** for Kratos' courier (`COURIER_SMTP_CONNECTION_URI`, `COURIER_SMTP_FROM_ADDRESS`; see `docs/LOCAL_DEV.md`), otherwise no reset emails are sent. Test it with `scripts/e2e-reset.sh`.

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
### Real Paystack TEST payment (runbook)

Test keys only: `PAYSTACK_SECRET_KEY` must be `sk_test_...` (the gateway refuses anything else).

```bash
make payments-real      # sets PAYMENTS_PROVIDER=paystack in .env, rebuilds+recreates gateway/payments
curl -s localhost:8080/graphql -H 'content-type: application/json' -d '{"query":"{health{paymentsProvider}}"}'   # -> paystack-test
scripts/payments-real-smoke.sh   # connects kofi's payout as a Paystack TEST subaccount (MTN MoMo 055 123 4987),
                                 # books kofi as demo.organizer, accepts it, calls initiateCheckout,
                                 # prints the https://checkout.paystack.com/... URL, checks verifyPayment is not paid
scripts/payments-webhook-proof.sh   # signed/unsigned/bad webhook proof against the local gateway (no Paystack needed)
make payments-fake      # back to the no-money fake provider when done
```

1. Run the smoke script, then in the app sign in as `demo.organizer@agrofie.local` / `changeme-seed-organizer`, Bookings: the new "Paystack TEST Hall" booking is `agreed`. Tap **Pay now** (or open the printed URL on your phone).
2. On Paystack's hosted page use a published test card ([Test Payments](https://docs-v2.paystack.com/docs/payments/test-payments/)): no-validation card `4084 0840 8408 4081`, any future expiry, CVV `408`. Other documented cards: PIN `1111` card `5078 5078 5078 5078 12` (CVV `081`); PIN+OTP card `5060 6666 6666 6666 666` (CVV `123`, PIN `1234`, OTP `123456`); PIN+phone+OTP card `5078 5078 5078 5078 04` (CVV `884`, PIN `0000`, OTP `123456`); declined `4084 0800 0000 5408` (CVV `001`). Mobile money test: MTN `055 123 4987`, no PIN/OTP.
3. Back in the app tap **I've paid** (the app also re-verifies automatically when it returns to the foreground). The booking should become `paid`. The server asks Paystack directly (`GET /transaction/verify/:reference`); the client is never trusted.
4. Paystack cannot reach localhost, so no webhook arrives; `verifyPayment` is the path. To receive real webhooks, tunnel the gateway (`cloudflared tunnel --url http://localhost:8080` or `ngrok http 8080`) and set the **test** webhook URL in the Paystack dashboard (Settings > API Keys & Webhooks) to `https://<tunnel-host>/webhooks/paystack`. Signature is HMAC-SHA512 of the raw body with the test secret key.
5. **Prerequisite:** Paystack rejects API calls (except `GET /bank`) from non-allowlisted IPs on accounts with IP whitelisting ("Your IP address is not allowed to make this call"). Disable the restriction for the test integration or add your current public IP in the dashboard (Settings > API Keys & Webhooks), then re-run the smoke script.

Notes: `PUBLIC_BASE_URL` is used for Paystack's `callback_url` (`/pay/return`, a back-to-app page) so it should be reachable from the phone. Talents' payout subaccounts created in fake mode (`ACCT_fake_...`) do not work with real Paystack: reconnect via the smoke script or the app. Run `scripts/e2e.sh` in fake mode.

- `DEV_ALLOW_EARLY_COMPLETE=true` (compose default) lets a paid booking be completed before its event date so reviews can be tested. Turn off for real use.
- There is no counter-offer mutation; talents accept or decline.
