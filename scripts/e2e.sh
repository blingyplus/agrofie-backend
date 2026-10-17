#!/usr/bin/env bash
# End-to-end walk of the whole product over GraphQL against a running stack
# (docker compose up --build -d). Requires jq and the admin seeded
# (make seed-admin). Expects the FAKE payment provider (PAYMENTS_PROVIDER=fake
# or no PAYSTACK_SECRET_KEY). Usage: scripts/e2e.sh [gateway-url]
set -uo pipefail
GW="${1:-${GATEWAY_URL:-http://localhost:8080}}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@agrofie.local}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-changeme-admin}"
PW="E2e-test-pw-$RANDOM-x"
RUN="$(date +%s)$RANDOM"
FAILS=0

gql() { # gql TOKEN QUERY [VARS_JSON]
  local tok="$1" q="$2" v="${3:-}"
  [ -n "$v" ] || v='{}'
  jq -n --arg q "$q" --argjson v "$v" '{query:$q,variables:$v}' |
    curl -sS "$GW/graphql" -H 'content-type: application/json' ${tok:+-H "Authorization: Bearer $tok"} -d @-
}
ok()   { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; FAILS=$((FAILS+1)); }
# expect NAME JSON JQ_EXPR EXPECTED
expect() { local got; got=$(echo "$2" | jq -r "$3" 2>/dev/null); if [ "$got" = "$4" ]; then ok "$1 ($got)"; else fail "$1: wanted '$4' got '$got'  $(echo "$2" | head -c 400)"; fi; }
val() { echo "$1" | jq -r "$2"; }
step() { echo; echo "== $1"; }
# Always remove the users/bookings this run created (@e2e.test) so Discover stays clean.
trap '"$(dirname "$0")/e2e-cleanup.sh"' EXIT

step "register + login (no email verification needed)"
R=$(gql "" 'mutation($i:RegisterInput!){register(input:$i){sessionToken user{id roleCodes}}}' "{\"i\":{\"email\":\"org$RUN@e2e.test\",\"password\":\"$PW\",\"displayName\":\"E2E Organizer\",\"roleCode\":\"organizer\"}}")
ORG=$(val "$R" .data.register.sessionToken); expect "organizer registered" "$R" '.data.register.user.roleCodes[0]' organizer
R=$(gql "" 'mutation($i:RegisterInput!){register(input:$i){sessionToken user{id roleCodes}}}' "{\"i\":{\"email\":\"tal$RUN@e2e.test\",\"password\":\"$PW\",\"displayName\":\"E2E Talent $RUN\",\"roleCode\":\"talent\"}}")
TAL=$(val "$R" .data.register.sessionToken); expect "talent registered" "$R" '.data.register.user.roleCodes[0]' talent
R=$(gql "" 'mutation($i:LoginInput!){login(input:$i){sessionToken}}' "{\"i\":{\"identifier\":\"tal$RUN@e2e.test\",\"password\":\"$PW\"}}")
expect "talent login" "$R" '.data.login.sessionToken|startswith("ory_st_")' true
R=$(gql "" 'mutation($i:LoginInput!){login(input:$i){sessionToken}}' "{\"i\":{\"identifier\":\"$ADMIN_EMAIL\",\"password\":\"$ADMIN_PASSWORD\"}}")
ADM=$(val "$R" .data.login.sessionToken); expect "admin login" "$R" '.data.login.sessionToken|startswith("ory_st_")' true

step "talent profile, rates, payout, availability"
R=$(gql "$TAL" 'mutation{updateMyTalentProfile(input:{headline:"E2E headline",bio:"bio",homePlaceCode:"greater_accra",genreCodes:["highlife"],typeCodes:["musician"],languageCodes:["en"],serviceAreaCodes:["greater_accra"]}){id displayName}}')
TID=$(val "$R" .data.updateMyTalentProfile.id); expect "profile updated" "$R" '.data.updateMyTalentProfile.id|length>10' true
R=$(gql "$TAL" 'mutation{setMyRates(rates:[{amount:"1000.00",currencyCode:"GHS",rateUnitCode:"per_event"}]){rates{amount rateUnitCode}}}')
expect "rate set" "$R" '.data.setMyRates.rates[0].amount' "1000.00"
R=$(gql "$TAL" 'query{settlementBanks(type:MOBILE_MONEY){code name}}'); expect "settlement banks" "$R" '.data.settlementBanks|length>0' true
BANK=$(val "$R" '.data.settlementBanks[0].code')
R=$(gql "$TAL" 'mutation($b:String!){connectMyPayoutAccount(input:{businessName:"E2E Talent",settlementType:MOBILE_MONEY,bankCode:$b,accountNumber:"0551234987"}){accountNumberLast4 bankName}}' "{\"b\":\"$BANK\"}")
expect "payout connected" "$R" '.data.connectMyPayoutAccount.accountNumberLast4' 4987
START=$(date -u -v+30d +%Y-%m-%dT10:00:00Z 2>/dev/null || date -u -d '+30 days' +%Y-%m-%dT10:00:00Z)
END=$(date -u -v+30d +%Y-%m-%dT16:00:00Z 2>/dev/null || date -u -d '+30 days' +%Y-%m-%dT16:00:00Z)
WS=$(date -u -v+20d +%Y-%m-%dT00:00:00Z 2>/dev/null || date -u -d '+20 days' +%Y-%m-%dT00:00:00Z)
WE=$(date -u -v+60d +%Y-%m-%dT00:00:00Z 2>/dev/null || date -u -d '+60 days' +%Y-%m-%dT00:00:00Z)
R=$(gql "$TAL" 'mutation($s:String!,$e:String!){addMyAvailabilityBlock(input:{startsAt:$s,endsAt:$e,isAvailable:true}){id}}' "{\"s\":\"$WS\",\"e\":\"$WE\"}")
expect "availability added" "$R" '.data.addMyAvailabilityBlock.id|length>10' true

step "not discoverable until verified"
R=$(gql "$ORG" 'query{searchTalent(first:100){items{id}}}'); 
echo "$R" | jq -e --arg t "$TID" '[.data.searchTalent.items[].id]|index($t)==null' >/dev/null && ok "unverified talent hidden" || fail "unverified talent visible"
R=$(gql "$ORG" 'mutation($t:ID!,$s:String!,$e:String!){requestBooking(input:{talentId:$t,startsAt:$s,endsAt:$e,rateUnitCode:"per_event"}){id}}' "{\"t\":\"$TID\",\"s\":\"$START\",\"e\":\"$END\"}")
echo "$R" | jq -e '.errors' >/dev/null && ok "booking an unverified talent rejected: $(val "$R" '.errors[0].message')" || fail "unverified talent was bookable"

step "verification + admin approval"
R=$(gql "$TAL" 'mutation{submitVerification(input:{typeCode:"identity",evidenceRef:"ghana-card-demo",notes:"e2e"}){id statusCode}}')
VID=$(val "$R" .data.submitVerification.id); expect "verification pending" "$R" '.data.submitVerification.statusCode' pending
R=$(gql "$ADM" 'query{pendingVerifications{id}}'); echo "$R" | jq -e --arg v "$VID" '[.data.pendingVerifications[].id]|index($v)!=null' >/dev/null && ok "appears in admin queue" || fail "not in admin queue $R"
R=$(gql "$ORG" 'query{pendingVerifications{id}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot see admin queue" || fail "organizer saw admin queue"
R=$(gql "$ADM" 'mutation($i:ID!){approveVerification(id:$i,reviewNotes:"ok"){statusCode}}' "{\"i\":\"$VID\"}"); expect "approved" "$R" '.data.approveVerification.statusCode' verified

step "discovery"
R=$(gql "$ORG" 'query{searchTalent(first:100,filter:{genreCodes:["highlife"]}){items{id displayName fromRate{amount}}}}')
echo "$R" | jq -e --arg t "$TID" '[.data.searchTalent.items[].id]|index($t)!=null' >/dev/null && ok "verified talent discoverable" || fail "not discoverable $R"
R=$(gql "$ORG" 'query($t:ID!){talent(id:$t){displayName rates{amount}}}' "{\"t\":\"$TID\"}"); expect "talent detail" "$R" '.data.talent.rates[0].amount' "1000.00"

book() { # book -> echoes booking id, creating a new inquiry at a distinct slot ($1 = day offset)
  local d="$1" s e
  s=$(date -u -v+${d}d +%Y-%m-%dT10:00:00Z 2>/dev/null || date -u -d "+$d days" +%Y-%m-%dT10:00:00Z)
  e=$(date -u -v+${d}d +%Y-%m-%dT16:00:00Z 2>/dev/null || date -u -d "+$d days" +%Y-%m-%dT16:00:00Z)
  gql "$ORG" 'mutation($t:ID!,$s:String!,$e:String!){requestBooking(input:{talentId:$t,eventTypeCode:"wedding",placeCode:"greater_accra",startsAt:$s,endsAt:$e,venueText:"E2E Hall",rateUnitCode:"per_event",note:"hello"}){id statusCode quotedAmount}}' "{\"t\":\"$TID\",\"s\":\"$s\",\"e\":\"$e\"}"
}

step "booking: request -> accept -> pay -> complete -> review"
R=$(book 30); B1=$(val "$R" .data.requestBooking.id); expect "inquiry created" "$R" '.data.requestBooking.statusCode' inquiry; expect "price from server rate" "$R" '.data.requestBooking.quotedAmount' "1000.00"
R=$(gql "$TAL" 'query{myBookingsAsTalent{id statusCode}}'); echo "$R" | jq -e --arg b "$B1" '[.data.myBookingsAsTalent[].id]|index($b)!=null' >/dev/null && ok "talent sees inquiry" || fail "talent inbox $R"
R=$(gql "$ORG" 'mutation($i:ID!){acceptBooking(id:$i){id}}' "{\"i\":\"$B1\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot accept" || fail "organizer accepted"
R=$(gql "$ORG" 'mutation($i:ID!){initiateCheckout(bookingId:$i){reference}}' "{\"i\":\"$B1\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "cannot pay before agreed" || fail "paid before agreed"
R=$(gql "$TAL" 'mutation($i:ID!){acceptBooking(id:$i){statusCode}}' "{\"i\":\"$B1\"}"); expect "talent accepted -> agreed" "$R" '.data.acceptBooking.statusCode' agreed
R=$(gql "$ORG" 'query($i:ID!){booking(id:$i){statusCode termsText}}' "{\"i\":\"$B1\"}"); expect "contract terms snapshot" "$R" '.data.booking.termsText|length>10' true
R=$(gql "$ORG" 'mutation($i:ID!){initiateCheckout(bookingId:$i){checkoutUrl reference amountPesewas commissionPesewas}}' "{\"i\":\"$B1\"}")
REF=$(val "$R" .data.initiateCheckout.reference); expect "checkout amount (pesewas)" "$R" '.data.initiateCheckout.amountPesewas' 100000; expect "commission 12.5%" "$R" '.data.initiateCheckout.commissionPesewas' 12500
expect "fake checkout url" "$R" '.data.initiateCheckout.checkoutUrl|contains("/pay/fake/")' true
CURL=$(val "$R" .data.initiateCheckout.checkoutUrl); CURL="$GW/pay/fake/$REF"
code=$(curl -s -o /dev/null -w '%{http_code}' "$CURL"); [ "$code" = 200 ] && ok "fake pay page served" || fail "pay page $code"
R=$(gql "$ORG" 'query($i:ID!){booking(id:$i){statusCode}}' "{\"i\":\"$B1\"}"); expect "still agreed before paying" "$R" '.data.booking.statusCode' agreed
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$GW/webhooks/paystack" -d "{\"event\":\"charge.success\",\"data\":{\"reference\":\"$REF\"}}"); [ "$code" = 400 ] && ok "unsigned webhook rejected (400)" || fail "unsigned webhook got $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$CURL" -d 'outcome=success'); [ "$code" = 200 ] && ok "fake pay (signed webhook path)" || fail "fake pay $code"
code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$CURL" -d 'outcome=success'); [ "$code" = 200 ] && ok "replay is idempotent" || fail "replay $code"
R=$(gql "$ORG" 'query($i:ID!){booking(id:$i){statusCode payment{statusCode amountPesewas}}}' "{\"i\":\"$B1\"}"); expect "booking paid" "$R" '.data.booking.statusCode' paid; expect "payment succeeded" "$R" '.data.booking.payment.statusCode' succeeded
R=$(gql "$ORG" 'mutation($i:ID!){completeBooking(id:$i){statusCode}}' "{\"i\":\"$B1\"}"); expect "completed" "$R" '.data.completeBooking.statusCode' completed
R=$(gql "$ORG" 'mutation($i:ID!){submitReview(input:{bookingId:$i,rating:5,body:"great"}){rating}}' "{\"i\":\"$B1\"}"); expect "organizer review" "$R" '.data.submitReview.rating' 5
R=$(gql "$TAL" 'mutation($i:ID!){submitReview(input:{bookingId:$i,rating:4,body:"nice org"}){rating}}' "{\"i\":\"$B1\"}"); expect "talent review" "$R" '.data.submitReview.rating' 4
R=$(gql "$ORG" 'mutation($i:ID!){submitReview(input:{bookingId:$i,rating:1}){rating}}' "{\"i\":\"$B1\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "duplicate review rejected" || fail "duplicate review allowed"
R=$(gql "$ORG" 'query($t:ID!){talentReviewSummary(talentId:$t){reviewCount averageRating}}' "{\"t\":\"$TID\"}"); expect "review summary" "$R" '.data.talentReviewSummary.reviewCount' 1

step "verifyPayment fallback (\"I've paid\") path + decline"
R=$(book 33); B2=$(val "$R" .data.requestBooking.id)
gql "$TAL" 'mutation($i:ID!){acceptBooking(id:$i){id}}' "{\"i\":\"$B2\"}" >/dev/null
gql "$ORG" 'mutation($i:ID!){initiateCheckout(bookingId:$i){reference}}' "{\"i\":\"$B2\"}" >/dev/null
R=$(gql "$ORG" 'mutation($i:ID!){verifyPayment(bookingId:$i){statusCode}}' "{\"i\":\"$B2\"}"); expect "verifyPayment succeeded" "$R" '.data.verifyPayment.statusCode' succeeded
R=$(gql "$ORG" 'query($i:ID!){booking(id:$i){statusCode}}' "{\"i\":\"$B2\"}"); expect "B2 paid via verify" "$R" '.data.booking.statusCode' paid
R=$(book 36); B3=$(val "$R" .data.requestBooking.id)
R=$(gql "$TAL" 'mutation($i:ID!){declineBooking(id:$i){statusCode}}' "{\"i\":\"$B3\"}"); expect "talent declined -> cancelled" "$R" '.data.declineBooking.statusCode' cancelled

step "counter-offers: counter -> counter back -> accept -> pay -> complete"
DET='id statusCode quotedAmount awaitingMyResponse lastOfferByRole counterCount offers{round byRole byMe amountPesewas note}'
R=$(book 39); B4=$(val "$R" .data.requestBooking.id)
R=$(gql "$ORG" 'query{myBookingsAsOrganizer{id awaitingMyResponse}}'); echo "$R" | jq -e --arg b "$B4" '.data.myBookingsAsOrganizer[]|select(.id==$b)|.awaitingMyResponse==false' >/dev/null && ok "organizer waits on inquiry" || fail "organizer awaiting flag $R"
R=$(gql "$TAL" 'query{myBookingsAsTalent{id awaitingMyResponse}}'); echo "$R" | jq -e --arg b "$B4" '.data.myBookingsAsTalent[]|select(.id==$b)|.awaitingMyResponse==true' >/dev/null && ok "talent owes a response" || fail "talent awaiting flag $R"
R=$(gql "$ORG" 'mutation($i:ID!){counterBooking(input:{bookingId:$i,amountPesewas:90000}){id}}' "{\"i\":\"$B4\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot counter own inquiry (not their turn)" || fail "organizer countered own inquiry"
R=$(gql "$TAL" 'mutation($i:ID!){counterBooking(input:{bookingId:$i,amountPesewas:0}){id}}' "{\"i\":\"$B4\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "zero amount rejected" || fail "zero amount accepted"
R=$(gql "$TAL" 'mutation($i:ID!){counterBooking(input:{bookingId:$i,amountPesewas:150000,currencyCode:"USD"}){id}}' "{\"i\":\"$B4\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "wrong currency rejected" || fail "USD accepted"
R=$(gql "$TAL" "mutation(\$i:ID!){counterBooking(input:{bookingId:\$i,amountPesewas:130000,note:\"Sound system included\"}){$DET}}" "{\"i\":\"$B4\"}")
expect "talent countered -> countered" "$R" '.data.counterBooking.statusCode' countered; expect "price updated" "$R" '.data.counterBooking.quotedAmount' "1300.00"
expect "talent no longer awaiting" "$R" '.data.counterBooking.awaitingMyResponse' false
R=$(gql "$TAL" 'mutation($i:ID!){counterBooking(input:{bookingId:$i,amountPesewas:125000}){id}}' "{\"i\":\"$B4\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "talent cannot counter twice in a row" || fail "talent double counter"
R=$(gql "$TAL" 'mutation($i:ID!){acceptBooking(id:$i){id}}' "{\"i\":\"$B4\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "talent cannot accept own counter" || fail "talent accepted own counter"
R=$(gql "$ORG" "mutation(\$i:ID!){counterBooking(input:{bookingId:\$i,amountPesewas:115000,note:\"Meet in the middle?\"}){$DET}}" "{\"i\":\"$B4\"}")
expect "organizer countered back" "$R" '.data.counterBooking.statusCode' countered; expect "history has 3 offers" "$R" '.data.counterBooking.offers|length' 3
expect "trail: byRole sequence" "$R" '[.data.counterBooking.offers[].byRole]|join(",")' organizer,talent,organizer; expect "counterCount" "$R" '.data.counterBooking.counterCount' 2
R=$(gql "$TAL" "query(\$i:ID!){booking(id:\$i){$DET}}" "{\"i\":\"$B4\"}"); expect "talent awaiting response" "$R" '.data.booking.awaitingMyResponse' true; expect "talent sees note in trail" "$R" '.data.booking.offers[1].note' "Sound system included"
R=$(gql "$ORG" 'mutation($i:ID!){acceptCounterOffer(id:$i){statusCode}}' "{\"i\":\"$B4\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot accept own counter" || fail "organizer accepted own counter"
R=$(gql "$TAL" 'mutation($i:ID!){acceptBooking(id:$i){statusCode quotedAmount}}' "{\"i\":\"$B4\"}"); expect "talent accepted -> agreed" "$R" '.data.acceptBooking.statusCode' agreed; expect "agreed at latest counter" "$R" '.data.acceptBooking.quotedAmount' "1150.00"
R=$(gql "$ORG" 'query($i:ID!){booking(id:$i){termsText}}' "{\"i\":\"$B4\"}"); expect "contract has negotiated price" "$R" '.data.booking.termsText|contains("1150.00")' true
R=$(gql "$TAL" 'mutation($i:ID!){counterBooking(input:{bookingId:$i,amountPesewas:100000}){id}}' "{\"i\":\"$B4\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "cannot counter an agreed booking" || fail "countered agreed booking"
R=$(gql "$ORG" 'mutation($i:ID!){initiateCheckout(bookingId:$i){reference amountPesewas}}' "{\"i\":\"$B4\"}"); REF4=$(val "$R" .data.initiateCheckout.reference); expect "checkout uses negotiated price" "$R" '.data.initiateCheckout.amountPesewas' 115000
curl -s -o /dev/null -X POST "$GW/pay/fake/$REF4" -d 'outcome=success'
R=$(gql "$ORG" 'query($i:ID!){booking(id:$i){statusCode}}' "{\"i\":\"$B4\"}"); expect "negotiated booking paid" "$R" '.data.booking.statusCode' paid
R=$(gql "$TAL" 'mutation($i:ID!){completeBooking(id:$i){statusCode}}' "{\"i\":\"$B4\"}"); expect "negotiated booking completed" "$R" '.data.completeBooking.statusCode' completed
R=$(book 42); B5=$(val "$R" .data.requestBooking.id)
gql "$TAL" 'mutation($i:ID!){counterBooking(input:{bookingId:$i,amountPesewas:120000}){id}}' "{\"i\":\"$B5\"}" >/dev/null
R=$(gql "$ORG" 'mutation($i:ID!){cancelBooking(id:$i){statusCode}}' "{\"i\":\"$B5\"}"); expect "organizer cancels a countered booking" "$R" '.data.cancelBooking.statusCode' cancelled
R=$(book 45); B6=$(val "$R" .data.requestBooking.id)
for n in 1 2 3 4 5 6; do
  if [ $((n%2)) = 1 ]; then T="$TAL"; else T="$ORG"; fi
  gql "$T" 'mutation($i:ID!,$a:Int!){counterBooking(input:{bookingId:$i,amountPesewas:$a}){id}}' "{\"i\":\"$B6\",\"a\":$((100000+n*1000))}" >/dev/null
done
R=$(gql "$TAL" 'mutation($i:ID!){counterBooking(input:{bookingId:$i,amountPesewas:99000}){id}}' "{\"i\":\"$B6\"}"); echo "$R" | jq -e '.errors[0].message|contains("maximum")' >/dev/null && ok "7th counter blocked (max rounds)" || fail "max rounds not enforced $R"

step "discover data: rating, featured, sorts"
R=$(gql "$ORG" 'query($t:ID!){talent(id:$t){featured ratingAverage reviewCount createdAt}}' "{\"t\":\"$TID\"}")
expect "detail rating (organizer review only)" "$R" '.data.talent.ratingAverage' 5; expect "detail reviewCount" "$R" '.data.talent.reviewCount' 1
expect "detail not featured" "$R" '.data.talent.featured' false; expect "createdAt is RFC3339" "$R" '.data.talent.createdAt|test("^[0-9]{4}-")' true
R=$(gql "$ORG" 'query{searchTalent(first:100,sort:TOP_RATED,filter:{genreCodes:["highlife"],minReviewCount:1}){items{id ratingAverage reviewCount}}}')
echo "$R" | jq -e --arg t "$TID" '[.data.searchTalent.items[]|select(.id==$t)|.reviewCount]==[1]' >/dev/null && ok "minReviewCount keeps rated talent" || fail "minReviewCount $R"
echo "$R" | jq -e '[.data.searchTalent.items[].ratingAverage]|. == (sort|reverse)' >/dev/null && ok "TOP_RATED sorted desc" || fail "TOP_RATED order $R"
R=$(gql "$ORG" 'mutation($t:ID!){setTalentFeatured(talentId:$t,featured:true,rank:1){id}}' "{\"t\":\"$TID\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot feature" || fail "organizer featured a talent"
R=$(gql "$TAL" 'mutation($t:ID!){setTalentFeatured(talentId:$t,featured:true,rank:1){id}}' "{\"t\":\"$TID\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "talent cannot self-feature" || fail "talent self-featured"
R=$(gql "$ADM" 'mutation($t:ID!){setTalentFeatured(talentId:$t,featured:true,rank:1){featured}}' "{\"t\":\"$TID\"}"); expect "admin features talent" "$R" '.data.setTalentFeatured.featured' true
R=$(gql "$ORG" 'query{searchTalent(first:100,sort:FEATURED,filter:{featuredOnly:true}){items{id featured}}}'); echo "$R" | jq -e --arg t "$TID" '[.data.searchTalent.items[]|select(.id==$t)]|length==1 and all(.[];.featured)' >/dev/null && ok "featuredOnly lists it" || fail "featuredOnly $R"
# page through 1-at-a-time to prove keyset pagination works with a non-default sort
N=0; AFTER=""; SEEN=""
for _ in $(seq 1 60); do
  R=$(gql "$ORG" 'query($a:String){searchTalent(first:1,sort:TOP_RATED,after:$a){items{id} nextCursor}}' "$(jq -n --arg a "$AFTER" 'if $a=="" then {} else {a:$a} end')")
  SEEN="$SEEN $(val "$R" '.data.searchTalent.items[0].id')"; N=$((N+1)); AFTER=$(val "$R" '.data.searchTalent.nextCursor // empty'); [ -n "$AFTER" ] || break
done
UNIQ=$(echo $SEEN | tr ' ' '\n' | sort -u | wc -l | tr -d ' '); [ "$UNIQ" = "$N" ] && [ "$N" -ge 2 ] && ok "TOP_RATED pagination: $N pages, no repeats" || fail "pagination pages=$N unique=$UNIQ"
R=$(gql "$ADM" 'mutation($t:ID!){setTalentFeatured(talentId:$t,featured:false){featured}}' "{\"t\":\"$TID\"}"); expect "admin unfeatures" "$R" '.data.setTalentFeatured.featured' false
R=$(gql "$ORG" 'query{talent(id:"'"$TID"'"){email phone}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "no contact fields on talent" || fail "contact fields exposed"

step "disputes + admin resolution"
R=$(gql "$ORG" 'mutation($i:ID!){openDispute(input:{bookingId:$i,reasonCode:"no_show",details:"Talent never arrived"}){id statusCode}}' "{\"i\":\"$B2\"}"); D=$(val "$R" .data.openDispute.id); expect "dispute opened" "$R" '.data.openDispute.statusCode' open
R=$(gql "$TAL" 'mutation($i:ID!){openDispute(input:{bookingId:$i,reasonCode:"other"}){id}}' "{\"i\":\"$B2\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "second open dispute rejected" || fail "two open disputes allowed"
R=$(gql "$ORG" 'query{disputes{id}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot list disputes" || fail "organizer listed disputes"
R=$(gql "$ADM" 'query{disputes(statusCode:"open"){id bookingStatusCode organizerDisplayName talentDisplayName}}'); echo "$R" | jq -e --arg d "$D" '[.data.disputes[].id]|index($d)!=null' >/dev/null && ok "dispute in admin queue" || fail "admin queue $R"
R=$(gql "$ADM" 'mutation($i:ID!){resolveDispute(id:$i,statusCode:"under_review"){statusCode}}' "{\"i\":\"$D\"}"); expect "under_review" "$R" '.data.resolveDispute.statusCode' under_review
R=$(gql "$ADM" 'mutation($i:ID!){resolveDispute(id:$i,statusCode:"resolved",resolutionNote:"Refund arranged outside the platform"){statusCode resolutionNote}}' "{\"i\":\"$D\"}"); expect "resolved" "$R" '.data.resolveDispute.statusCode' resolved
R=$(gql "$TAL" 'query($i:ID!){booking(id:$i){disputes{statusCode resolutionNote}}}' "{\"i\":\"$B2\"}"); expect "talent sees resolution" "$R" '.data.booking.disputes[0].resolutionNote|length>0' true
R=$(gql "$ORG" 'mutation($i:ID!){openDispute(input:{bookingId:$i,reasonCode:"quality"}){statusCode}}' "{\"i\":\"$B2\"}"); expect "new dispute allowed after resolve" "$R" '.data.openDispute.statusCode' open

step "talent requests (\"Tell us what you need\")"
RD=$(date -u -v+50d +%Y-%m-%d 2>/dev/null || date -u -d '+50 days' +%Y-%m-%d)
R=$(gql "$TAL" 'mutation{createTalentRequest(input:{note:"x"}){id}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "talent cannot create a request" || fail "talent created a request"
R=$(gql "$ORG" 'mutation{createTalentRequest(input:{}){id}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "empty request rejected" || fail "empty request accepted"
R=$(gql "$ORG" 'mutation{createTalentRequest(input:{genreCode:"nope"}){id}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "unknown genre rejected" || fail "unknown genre accepted"
R=$(gql "$ORG" 'mutation($d:String!){createTalentRequest(input:{eventTypeCode:"wedding",genreCode:"highlife",placeCode:"greater_accra",eventDate:$d,budgetPesewas:250000,note:"Live band for 200 guests"}){id statusCode genreName placeName eventTypeName budgetPesewas eventDate}}' "{\"d\":\"$RD\"}")
TR=$(val "$R" .data.createTalentRequest.id); expect "request created open" "$R" '.data.createTalentRequest.statusCode' open; expect "request budget (pesewas)" "$R" '.data.createTalentRequest.budgetPesewas' 250000; expect "request place resolved" "$R" '.data.createTalentRequest.placeName|length>0' true
R=$(gql "$ORG" 'query{myTalentRequests{id statusCode}}'); expect "organizer sees own request" "$R" '.data.myTalentRequests|length' 1
R=$(gql "$TAL" 'query{myTalentRequests{id}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "talent cannot list requests" || fail "talent listed requests"
R=$(gql "$ORG" 'query{talentRequests{id}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot see admin requests" || fail "organizer saw admin requests"
R=$(gql "$ADM" 'query{talentRequests(statusCode:"open"){id organizerDisplayName}}'); echo "$R" | jq -e --arg r "$TR" '[.data.talentRequests[].id]|index($r)!=null' >/dev/null && ok "request in admin queue" || fail "admin requests $R"
R=$(gql "$ORG" 'mutation($i:ID!){setTalentRequestStatus(id:$i,statusCode:"matched"){id}}' "{\"i\":\"$TR\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot set status" || fail "organizer set status"
R=$(gql "$ADM" 'mutation($i:ID!){setTalentRequestStatus(id:$i,statusCode:"matched"){statusCode}}' "{\"i\":\"$TR\"}"); expect "admin marks matched" "$R" '.data.setTalentRequestStatus.statusCode' matched
R=$(gql "$ADM" 'mutation($i:ID!){setTalentRequestStatus(id:$i,statusCode:"bogus"){statusCode}}' "{\"i\":\"$TR\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "bad status rejected" || fail "bad status accepted"

step "demo seed (skipped if make seed-demo not run)"
R=$(gql "" 'mutation($i:LoginInput!){login(input:$i){sessionToken}}' '{"i":{"identifier":"demo.organizer@agrofie.local","password":"changeme-seed-organizer"}}')
DORG=$(val "$R" .data.login.sessionToken)
if [[ "$DORG" == ory_st_* ]]; then
  R=$(gql "$DORG" 'query{searchTalent(first:50){items{id displayName fromRate{amount}}}}'); expect "seeded talents discoverable" "$R" '.data.searchTalent.items|length>=11' true
  SID=$(val "$R" '.data.searchTalent.items[]|select(.displayName=="DJ Kofi Mensimah")|.id')
  S=$(date -u -v+45d +%Y-%m-%dT10:00:00Z 2>/dev/null || date -u -d '+45 days' +%Y-%m-%dT10:00:00Z); E=$(date -u -v+45d +%Y-%m-%dT16:00:00Z 2>/dev/null || date -u -d '+45 days' +%Y-%m-%dT16:00:00Z)
  R=$(gql "$DORG" 'mutation($t:ID!,$s:String!,$e:String!){requestBooking(input:{talentId:$t,startsAt:$s,endsAt:$e,rateUnitCode:"per_event"}){statusCode quotedAmount}}' "{\"t\":\"$SID\",\"s\":\"$S\",\"e\":\"$E\"}"); expect "seeded talent bookable" "$R" '.data.requestBooking.quotedAmount' "1800.00"
  gql "$DORG" 'query{myBookingsAsOrganizer{id}}' >/dev/null
else echo "  (demo organizer not seeded; skipping)"; fi

echo
if [ "$FAILS" -eq 0 ]; then echo "E2E PASSED"; else echo "E2E: $FAILS FAILURE(S)"; exit 1; fi
