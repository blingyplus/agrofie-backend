#!/usr/bin/env bash
# Real Paystack TEST-mode smoke: needs `make payments-real` first. Uses the seeded demo
# accounts (make seed-demo): connects kofi's payout as a Paystack TEST subaccount (MTN
# MoMo, Paystack's published test number), creates a booking as demo.organizer, kofi
# accepts it (agreed), then initiateCheckout and verifyPayment (expects still pending).
# Prints the checkout URL to open on your phone. It never touches card details.
# Usage: scripts/payments-real-smoke.sh [gateway-url]
set -uo pipefail
GW="${1:-${GATEWAY_URL:-http://localhost:8080}}"
gql() { local v="${3:-}"; [ -n "$v" ] || v='{}'; jq -n --arg q "$2" --argjson v "$v" '{query:$q,variables:$v}' | curl -sS "$GW/graphql" -H 'content-type: application/json' ${1:+-H "Authorization: Bearer $1"} -d @-; }
die() { echo "FAIL: $1" >&2; exit 1; }
login() { gql "" 'mutation($i:LoginInput!){login(input:$i){sessionToken}}' "{\"i\":{\"identifier\":\"$1\",\"password\":\"$2\"}}" | jq -r .data.login.sessionToken; }

P=$(gql "" '{health{paymentsProvider}}' | jq -r .data.health.paymentsProvider)
[ "$P" = paystack-test ] || die "provider is '$P'; run: make payments-real"
ORG=$(login demo.organizer@agrofie.local changeme-seed-organizer); TAL=$(login seed.kofi-mensimah@agrofie.local changeme-seed-talent)
[ "${ORG#ory_st_}" != "$ORG" ] && [ "${TAL#ory_st_}" != "$TAL" ] || die "demo login failed (make seed-demo?)"

# MTN Mobile Money test number published in Paystack's "Test Payments" docs: 055 123 4987.
R=$(gql "$TAL" 'mutation{connectMyPayoutAccount(input:{businessName:"Kofi Mensimah (TEST)",settlementType:MOBILE_MONEY,bankCode:"MTN",accountNumber:"0551234987"}){accountName bankName accountNumberLast4}}')
echo "connectMyPayoutAccount: $(echo "$R" | jq -c '.data // .errors')"
echo "$R" | jq -e .data.connectMyPayoutAccount >/dev/null || die "payout connect failed"

TID=$(gql "$TAL" '{myTalentProfile{id}}' | jq -r .data.myTalentProfile.id)
D=$(( 10 + RANDOM % 60 ))
S=$(date -u -v+${D}d +%Y-%m-%dT10:00:00Z 2>/dev/null || date -u -d "+$D days" +%Y-%m-%dT10:00:00Z)
E=$(date -u -v+${D}d +%Y-%m-%dT16:00:00Z 2>/dev/null || date -u -d "+$D days" +%Y-%m-%dT16:00:00Z)
R=$(gql "$ORG" 'mutation($t:ID!,$s:String!,$e:String!){requestBooking(input:{talentId:$t,eventTypeCode:"wedding",placeCode:"greater_accra",startsAt:$s,endsAt:$e,venueText:"Paystack TEST Hall",rateUnitCode:"per_event",note:"paystack test"}){id statusCode quotedAmount}}' "{\"t\":\"$TID\",\"s\":\"$S\",\"e\":\"$E\"}")
echo "requestBooking: $(echo "$R" | jq -c '.data // .errors')"
B=$(echo "$R" | jq -r .data.requestBooking.id); [ "$B" != null ] || die "requestBooking failed"
gql "$TAL" 'mutation($i:ID!){acceptBooking(id:$i){statusCode}}' "{\"i\":\"$B\"}" | jq -c '.data // .errors'
R=$(gql "$ORG" 'mutation($i:ID!){initiateCheckout(bookingId:$i){checkoutUrl reference amountPesewas commissionPesewas}}' "{\"i\":\"$B\"}")
echo "initiateCheckout: $(echo "$R" | jq -c '.data // .errors')"
URL=$(echo "$R" | jq -r .data.initiateCheckout.checkoutUrl); case "$URL" in https://checkout.paystack.com/*) ;; *) die "no paystack checkout url";; esac
gql "$ORG" 'mutation($i:ID!){verifyPayment(bookingId:$i){statusCode amountPesewas}}' "{\"i\":\"$B\"}" | jq -c '"verifyPayment: " + ((.data // .errors)|tostring)' -r
echo; echo "booking id: $B"; echo "OPEN ON YOUR PHONE: $URL"
