# Known vulnerability exceptions

Last checked during scaffold setup.

## Backend

`make vuln` (`govulncheck ./...`) — **No vulnerabilities found** after pinning `golang.org/x/text@v0.39.0`.

## Client

`npm audit --omit=dev` — **0 vulnerabilities** with npm overrides:

- `decode-uri-component` → `0.5.0` (Expo Router → query-string)
- `uuid` → `^11.1.1` (Expo config-plugins → xcode)

Do **not** run `npm audit fix --force` (it downgrades Expo off SDK 57). Re-verify overrides after each Expo upgrade.

Dependabot is configured weekly on both repos.
