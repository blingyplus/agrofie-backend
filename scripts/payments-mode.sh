#!/usr/bin/env bash
# Flip the payment provider in .env and recreate the gateway (checkout, webhook and
# payout subaccounts run in the gateway; the payments service is a health stub).
# Usage: scripts/payments-mode.sh real|fake      (or: make payments-real / payments-fake)
# real = Paystack TEST API using PAYSTACK_SECRET_KEY (must be sk_test_...). Never live keys.
set -euo pipefail
cd "$(dirname "$0")/.."
mode="${1:-}"
case "$mode" in real|fake) ;; *) echo "usage: $0 real|fake" >&2; exit 2;; esac
[ -f .env ] || { echo ".env not found" >&2; exit 1; }

if [ "$mode" = real ]; then
  key=$(grep -E '^PAYSTACK_SECRET_KEY=' .env | tail -1 | cut -d= -f2- || true)
  case "$key" in sk_test_?*) ;; *) echo "PAYSTACK_SECRET_KEY in .env must be a Paystack TEST key (sk_test_...)." >&2; exit 1;; esac
  val=paystack
else
  val=fake
fi

# Replace (or append) PAYMENTS_PROVIDER in .env without touching other lines.
tmp=$(mktemp)
if grep -qE '^PAYMENTS_PROVIDER=' .env; then
  sed -E "s/^PAYMENTS_PROVIDER=.*/PAYMENTS_PROVIDER=$val/" .env > "$tmp"
else
  { cat .env; echo "PAYMENTS_PROVIDER=$val"; } > "$tmp"
fi
cat "$tmp" > .env; rm -f "$tmp"

docker compose up -d --build --force-recreate gateway payments
sleep 3
echo "PAYMENTS_PROVIDER=$val ->"
docker compose logs --tail 20 gateway 2>&1 | grep -E 'payments provider|REAL|FAKE' | sed 's/^[^|]*| *//' || true
