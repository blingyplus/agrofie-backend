#!/usr/bin/env bash
# Proves the Paystack webhook path locally in real (TEST) mode without Paystack reaching us:
# builds a charge.success body, signs it (HMAC-SHA512) with PAYSTACK_SECRET_KEY from .env
# (never printed), posts it to /webhooks/paystack and checks: bad/unsigned/fake-key -> 400,
# good -> 200 and booking agreed -> paid, replay -> 200 no-op. Uses a synthetic payments row
# on a throwaway demo booking and removes it afterwards. Needs `make payments-real`.
set -uo pipefail
cd "$(dirname "$0")/.."
GW="${1:-http://localhost:8080}"
KEY=$(grep -E '^PAYSTACK_SECRET_KEY=' .env | cut -d= -f2-)
gql() { local v="${3:-}"; [ -n "$v" ] || v='{}'; jq -n --arg q "$2" --argjson v "$v" '{query:$q,variables:$v}' | curl -sS "$GW/graphql" -H 'content-type: application/json' ${1:+-H "Authorization: Bearer $1"} -d @-; }
login() { gql "" 'mutation($i:LoginInput!){login(input:$i){sessionToken}}' "{\"i\":{\"identifier\":\"$1\",\"password\":\"$2\"}}" | jq -r .data.login.sessionToken; }
psql_() { docker compose exec -T postgres psql -U agrofie -d agrofie -qAt "$@"; }
sign() { printf %s "$1" | openssl dgst -sha512 -hmac "$2" | awk '{print $NF}'; }
post() { curl -s -o /dev/null -w %{http_code} -X POST "$GW/webhooks/paystack" ${2:+-H "x-paystack-signature: $2"} -d "$1"; }
FAILS=0; check() { if [ "$2" = "$3" ]; then echo "  ok   $1 ($2)"; else echo "  FAIL $1: wanted $3 got $2"; FAILS=$((FAILS+1)); fi; }

[ "$(gql "" '{health{paymentsProvider}}' | jq -r .data.health.paymentsProvider)" = paystack-test ] || { echo "run: make payments-real"; exit 1; }
ORG=$(login demo.organizer@agrofie.local changeme-seed-organizer); TAL=$(login seed.kofi-mensimah@agrofie.local changeme-seed-talent)
TID=$(gql "$TAL" '{myTalentProfile{id}}' | jq -r .data.myTalentProfile.id)
D=$((10 + RANDOM % 60))
S=$(date -u -v+${D}d +%Y-%m-%dT10:00:00Z 2>/dev/null || date -u -d "+$D days" +%Y-%m-%dT10:00:00Z)
E=$(date -u -v+${D}d +%Y-%m-%dT16:00:00Z 2>/dev/null || date -u -d "+$D days" +%Y-%m-%dT16:00:00Z)
R=$(gql "$ORG" 'mutation($t:ID!,$s:String!,$e:String!){requestBooking(input:{talentId:$t,eventTypeCode:"wedding",placeCode:"greater_accra",startsAt:$s,endsAt:$e,venueText:"Webhook proof",rateUnitCode:"per_event"}){id}}' "{\"t\":\"$TID\",\"s\":\"$S\",\"e\":\"$E\"}")
B=$(echo "$R" | jq -r .data.requestBooking.id); [ "$B" != null ] || { echo "requestBooking failed: $(echo "$R" | head -c 300)"; exit 1; }
gql "$TAL" 'mutation($i:ID!){acceptBooking(id:$i){statusCode}}' "{\"i\":\"$B\"}" >/dev/null
status() { gql "$ORG" 'query($i:ID!){booking(id:$i){statusCode}}' "{\"i\":\"$B\"}" | jq -r '.data.booking.statusCode // "?"'; }
REF="agrofie_whproof_$RANDOM$RANDOM"
psql_ -c "INSERT INTO payments(booking_id,payment_status_id,provider_reference,amount_pesewas,commission_pesewas,currency_code) SELECT '$B',id,'$REF',100000,12500,'GHS' FROM payment_statuses WHERE code='pending'" >/dev/null
BODY=$(jq -cn --arg r "$REF" '{event:"charge.success",data:{id:1,reference:$r,status:"success",amount:100000,currency:"GHS",channel:"card"}}')
check "bad signature rejected" "$(post "$BODY" deadbeef)" 400
check "unsigned rejected" "$(post "$BODY")" 400
check "fake-provider key signature rejected" "$(post "$BODY" "$(sign "$BODY" agrofie-fake-provider-test-only)")" 400
check "booking still agreed" "$(status)" agreed
BAD=$(echo "$BODY" | jq -c '.data.amount=5')
check "signed but wrong amount rejected" "$(post "$BAD" "$(sign "$BAD" "$KEY")")" 400
check "booking still agreed after wrong amount" "$(status)" agreed
SIG=$(sign "$BODY" "$KEY")
check "valid signature accepted" "$(post "$BODY" "$SIG")" 200
check "booking now paid" "$(status)" paid
check "replay is a no-op" "$(post "$BODY" "$SIG")" 200
check "one event row" "$(psql_ -c "select count(*) from payment_events where provider_reference='$REF'")" 1
# Unknown reference: must fail loudly (500 so Paystack retries) and NOT poison idempotency.
UB=$(jq -cn '{event:"charge.success",data:{reference:"agrofie_unknown_ref",status:"success"}}')
check "unknown reference -> 500" "$(post "$UB" "$(sign "$UB" "$KEY")")" 500
check "failed event not left as handled" "$(psql_ -c "select count(*) from payment_events where provider_reference='agrofie_unknown_ref'")" 0

# cleanup the throwaway rows (the paid booking stays in the demo data otherwise)
psql_ -c "DELETE FROM payment_events WHERE provider_reference='$REF'; DELETE FROM payments WHERE booking_id='$B';" >/dev/null
psql_ -c "DELETE FROM bookings WHERE id='$B'" >/dev/null 2>&1 || echo "  note: booking $B kept (has dependent rows); remove manually if unwanted"
[ $FAILS -eq 0 ] && echo "WEBHOOK PROOF PASSED" || { echo "WEBHOOK PROOF FAILED ($FAILS)"; exit 1; }
