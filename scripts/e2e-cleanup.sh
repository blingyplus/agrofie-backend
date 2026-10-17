#!/usr/bin/env bash
# Removes everything scripts/e2e.sh created (users whose email ends in @e2e.test)
# plus their bookings, payments, disputes, reviews and Kratos identities, so the
# runs never pollute Discover. Local dev stack only (docker compose postgres +
# Kratos admin on localhost:4434). Extra patterns can be passed as
# "EMAIL_LIKE" arguments (e.g. scripts/e2e-cleanup.sh 'kofi.talent1@example.com').
# Usage: scripts/e2e-cleanup.sh [extra-email-like ...]
set -uo pipefail
cd "$(dirname "$0")/.."
KRATOS_ADMIN="${KRATOS_ADMIN_URL:-http://localhost:4434}"
PATTERNS=('%@e2e.test' "$@")
ARR=$(printf "'%s'," "${PATTERNS[@]}"); ARR="ARRAY[${ARR%,}]"
psql_() { docker compose exec -T postgres psql -U agrofie -d agrofie -v ON_ERROR_STOP=1 -At "$@"; }

IDS=$(psql_ -c "SELECT kratos_identity_id FROM users WHERE email LIKE ANY($ARR)") || { echo "cleanup: postgres unavailable, skipping"; exit 0; }

psql_ <<SQL >/dev/null
BEGIN;
CREATE TEMP TABLE _u ON COMMIT DROP AS SELECT id FROM users WHERE email LIKE ANY($ARR);
CREATE TEMP TABLE _b ON COMMIT DROP AS
  SELECT b.id FROM bookings b
  WHERE b.organizer_user_id IN (SELECT id FROM _u)
     OR b.talent_profile_id IN (SELECT id FROM talent_profiles WHERE user_id IN (SELECT id FROM _u));
-- Order matters: disputes/payments have no ON DELETE CASCADE to bookings.
DELETE FROM disputes WHERE booking_id IN (SELECT id FROM _b) OR opened_by_user_id IN (SELECT id FROM _u) OR resolved_by_user_id IN (SELECT id FROM _u);
DELETE FROM payments WHERE booking_id IN (SELECT id FROM _b);
DELETE FROM bookings WHERE id IN (SELECT id FROM _b);  -- cascades events, contracts, reviews of those bookings
DELETE FROM reviews WHERE author_user_id IN (SELECT id FROM _u);
DELETE FROM booking_status_events WHERE actor_user_id IN (SELECT id FROM _u);
UPDATE verifications SET reviewed_by = NULL WHERE reviewed_by IN (SELECT id FROM _u);
DELETE FROM password_reset_requests WHERE email LIKE ANY($ARR);
DELETE FROM users WHERE id IN (SELECT id FROM _u);      -- cascades profiles, talent_*, rates, payout, availability
COMMIT;
SQL

n=0
for id in $IDS; do
  curl -s -o /dev/null -X DELETE "$KRATOS_ADMIN/admin/identities/$id" && n=$((n+1))
done
echo "e2e cleanup: removed $n test user(s)"
