# Stack & packages

## Client (`agrofie-client`)

| Package | Why |
| --- | --- |
| Expo SDK 57 + Expo Router | Single codebase iOS/Android/web |
| TypeScript | Spec mandate |
| NativeWind + Tailwind 3 | Shared styling without CSS-in-JS sprawl |
| urql + graphql 16 | Lean GraphQL client (low bandwidth) |
| graphql-codegen | Types from `agrofie-backend/graph/schema.graphqls` |
| zustand | UI session only |
| zod + react-hook-form | Forms |
| expo-secure-store / image / calendar / localization | Platform primitives |
| i18next | `en` now, Twi later |

Install native modules with:

```bash
npx expo install <pkg>
```

Generate types:

```bash
npm run codegen
```

## Backend (`agrofie-backend`)

| Package / tool | Why |
| --- | --- |
| Go 1.25 | Spec concurrency choice |
| pgx/v5 | Postgres driver |
| golang-migrate | SQL migrations |
| sqlc (`make sqlc`) | Typed queries from SQL |
| Buf + Connect (`make proto`) | Internal RPC contracts |
| gqlgen config present | Evolve hand-rolled GraphQL dispatcher → generated resolvers |
| slog | Structured logs |
| govulncheck | Vulnerability scanning |

## Vulnerability policy

- Client: `npm audit --omit=dev`; Prefer `expo install` pins; Dependabot weekly. See [VULNERABILITIES.md](./VULNERABILITIES.md).
- Backend: `make vuln` (`govulncheck ./...`); Dependabot for gomod/docker.
- No secrets in git. `.env.example` only.

## Explicitly rejected

Vuexy / Next 13 Material UI template, MongoDB, raw Stripe (Ghana MoMo → Paystack), hand-rolled JWT from legacy Agermax.
