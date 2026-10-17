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

step "public app config (GET /config)"
R=$(curl -sS -i "$GW/config"); echo "$R" | grep -qi '^cache-control:' && ok "config cache-control" || fail "config cache-control missing"
R=$(curl -sS "$GW/config"); expect "config version" "$R" .version 1
expect "config invite derives from base" "$R" '(.webBaseUrl as $b | .inviteUrl | startswith($b))' true
expect "config profile template" "$R" '.profileUrlTemplate|contains("{id}")' true
expect "config has flags + maintenance" "$R" '(.featureFlags|type)+(.maintenance.enabled|tostring)' objectfalse

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

step "media: upload, attach, public vs private access"
TMPD=$(mktemp -d); trap 'rm -rf "$TMPD"; "$(dirname "$0")/e2e-cleanup.sh"' EXIT
# 8x8 PNG and a minimal PDF, generated here (no fixtures on disk).
PNG_B64='iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAIAAABLbSncAAAAEUlEQVR4nGM4YROFFTEMLQkATEZXgWfmY7oAAAAASUVORK5CYII='
printf '%s' "$PNG_B64" | { base64 -d 2>/dev/null || base64 -D; } > "$TMPD/p.png"
printf '%%PDF-1.4\n1 0 obj<</Type/Catalog>>endobj\ntrailer<</Root 1 0 R>>\n%%%%EOF\n' > "$TMPD/d.pdf"
printf 'MZ\x90\x00 not an image' > "$TMPD/evil.png"
up() { curl -sS -o "$TMPD/out.json" -w '%{http_code}' "$GW/media" ${1:+-H "Authorization: Bearer $1"} -F "kind=$2" -F "file=@$3${4:+;type=$4}"; }
code() { curl -sS -o /dev/null -w '%{http_code}' "$@"; }
C=$(up "" profile_photo "$TMPD/p.png"); [ "$C" = 401 ] && ok "anonymous upload rejected (401)" || fail "anonymous upload got $C"
C=$(up "$TAL" profile_photo "$TMPD/evil.png" image/png); [ "$C" = 415 ] && ok "disguised non-image rejected (415)" || fail "evil.png got $C"
C=$(up "$TAL" profile_photo "$TMPD/d.pdf" application/pdf); [ "$C" = 400 ] && ok "PDF refused for profile photo (400)" || fail "pdf as photo got $C"
C=$(up "$TAL" bogus_kind "$TMPD/p.png"); [ "$C" = 400 ] && ok "unknown kind rejected (400)" || fail "bogus kind got $C"
C=$(up "$TAL" profile_photo "$TMPD/p.png"); PHOTO=$(jq -r .id "$TMPD/out.json")
[ "$C" = 201 ] && ok "photo uploaded ($PHOTO)" || fail "photo upload got $C $(cat "$TMPD/out.json")"
jq -e '.variants.thumb and .variants.medium and (.url|startswith("http"))' "$TMPD/out.json" >/dev/null && ok "response has url + variants" || fail "bad upload response"
C=$(up "$TAL" cover_image "$TMPD/p.png"); COVER=$(jq -r .id "$TMPD/out.json"); [ "$C" = 201 ] && ok "cover uploaded" || fail "cover upload got $C"
C=$(up "$TAL" verification_document "$TMPD/d.pdf" application/pdf); DOC=$(jq -r .id "$TMPD/out.json"); DOCURL=$(jq -r .url "$TMPD/out.json")
[ "$C" = 201 ] && ok "verification PDF uploaded" || fail "pdf upload got $C $(cat "$TMPD/out.json")"
C=$(up "$ORG" verification_document "$TMPD/d.pdf" application/pdf); ORGDOC=$(jq -r .id "$TMPD/out.json")

R=$(gql "$ORG" 'mutation($m:ID){setTalentPhoto(mediaId:$m){id}}' "{\"m\":\"$PHOTO\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot set talent photo (role)" || fail "organizer set photo"
R=$(gql "$ADM" 'mutation($m:ID){setTalentPhoto(mediaId:$m){id}}' "{\"m\":\"$PHOTO\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "admin cannot set talent photo (role)" || fail "admin set photo"
R=$(gql "$TAL" 'mutation($m:ID){setTalentPhoto(mediaId:$m){id}}' "{\"m\":\"$DOC\"}"); echo "$R" | jq -e '.errors' >/dev/null && ok "document cannot be used as photo" || fail "doc used as photo"
R=$(gql "$TAL" 'mutation($m:ID){setTalentPhoto(mediaId:$m){id photoUrl}}' "{\"m\":\"$PHOTO\"}"); expect "photo attached" "$R" '.data.setTalentPhoto.photoUrl|contains("/media/'"$PHOTO"'")' true
R=$(gql "$TAL" 'mutation($m:ID){setTalentCover(mediaId:$m){coverUrl}}' "{\"m\":\"$COVER\"}"); expect "cover attached" "$R" '.data.setTalentCover.coverUrl|contains("/media/'"$COVER"'")' true

[ "$(code "$GW/media/$PHOTO?v=thumb")" = 200 ] && ok "public photo served without auth" || fail "public photo not served"
curl -sSI "$GW/media/$PHOTO?v=medium" | grep -qi 'cache-control: public, max-age=31536000' && ok "public photo has long cache headers" || fail "no cache header"
[ "$(curl -sS "$GW/media/$PHOTO?v=thumb" | file - | grep -ci jpeg)" = 1 ] && ok "served variant is JPEG" || fail "variant not jpeg"
[ "$(code "$GW/media/$DOC")" = 401 ] && ok "private doc: anonymous -> 401" || fail "private doc open to anonymous"
[ "$(code -H "Authorization: Bearer $ORG" "$GW/media/$DOC")" = 404 ] && ok "private doc: other user -> 404" || fail "private doc visible to other user"
[ "$(code -H "Authorization: Bearer $TAL" "$GW/media/$DOC")" = 200 ] && ok "private doc: owner -> 200" || fail "owner cannot open own doc"
[ "$(code -H "Authorization: Bearer $ADM" "$GW/media/$DOC")" = 200 ] && ok "private doc: admin -> 200" || fail "admin cannot open doc"
[ "$(code "$DOCURL")" = 200 ] && ok "signed URL opens private doc" || fail "signed url failed"
[ "$(code "${DOCURL%sig=*}sig=deadbeef")" = 401 ] && ok "tampered signature rejected" || fail "tampered signature accepted"
[ "$(code "$GW/media/..%2f..%2fetc%2fpasswd")" = 404 ] && ok "path traversal -> 404" || fail "traversal not 404"
R=$(gql "$TAL" 'mutation($i:SubmitVerificationInput!){submitVerification(input:$i){id}}' "{\"i\":{\"typeCode\":\"identity\",\"mediaId\":\"$ORGDOC\"}}"); echo "$R" | jq -e '.errors' >/dev/null && ok "cannot attach someone else's document" || fail "attached foreign document"

step "verification + admin approval"
R=$(gql "$TAL" 'mutation($m:ID){submitVerification(input:{typeCode:"identity",mediaId:$m,notes:"e2e"}){id statusCode evidenceMedia{id mime}}}' "{\"m\":\"$DOC\"}")
expect "evidence media on verification" "$R" '.data.submitVerification.evidenceMedia.mime' application/pdf
R2=$(gql "$TAL" 'mutation{submitVerification(input:{typeCode:"business",evidenceRef:"https://example.com/legacy-link"}){statusCode evidenceRef}}'); expect "legacy evidenceRef still works" "$R2" '.data.submitVerification.evidenceRef' https://example.com/legacy-link
VID=$(val "$R" .data.submitVerification.id); expect "verification pending" "$R" '.data.submitVerification.statusCode' pending
R=$(gql "$ADM" 'query{pendingVerifications{id evidenceMedia{id mime url}}}'); echo "$R" | jq -r --arg v "$VID" '.data.pendingVerifications[]|select(.id==$v)|.evidenceMedia.url' > "$TMPD/evurl"
[ "$(code "$(cat "$TMPD/evurl")")" = 200 ] && ok "admin queue exposes openable evidence url" || fail "evidence url not openable"
R=$(gql "$ADM" 'query{pendingVerifications{id}}'); echo "$R" | jq -e --arg v "$VID" '[.data.pendingVerifications[].id]|index($v)!=null' >/dev/null && ok "appears in admin queue" || fail "not in admin queue $R"
R=$(gql "$ORG" 'query{pendingVerifications{id}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "organizer cannot see admin queue" || fail "organizer saw admin queue"
R=$(gql "$ADM" 'mutation($i:ID!){approveVerification(id:$i,reviewNotes:"ok"){statusCode}}' "{\"i\":\"$VID\"}"); expect "approved" "$R" '.data.approveVerification.statusCode' verified

step "discovery"
R=$(gql "$ORG" 'query{searchTalent(first:100,filter:{genreCodes:["highlife"]}){items{id displayName fromRate{amount}}}}')
echo "$R" | jq -e --arg t "$TID" '[.data.searchTalent.items[].id]|index($t)!=null' >/dev/null && ok "verified talent discoverable" || fail "not discoverable $R"
R=$(gql "$ORG" 'query($t:ID!){talent(id:$t){displayName rates{amount}}}' "{\"t\":\"$TID\"}"); expect "talent detail" "$R" '.data.talent.rates[0].amount' "1000.00"
R=$(gql "$ORG" 'query{searchTalent(first:100){items{id photoUrl coverUrl}}}')
echo "$R" | jq -e --arg t "$TID" '.data.searchTalent.items[]|select(.id==$t)|.photoUrl!=null' >/dev/null && ok "Discover card carries photoUrl" || fail "no photoUrl on Discover card"
R=$(gql "$ORG" 'query($t:ID!){talent(id:$t){photoUrl coverUrl}}' "{\"t\":\"$TID\"}"); expect "detail has coverUrl" "$R" '.data.talent.coverUrl|length>0' true
[ "$(code -X DELETE -H "Authorization: Bearer $ORG" "$GW/media/$COVER")" = 404 ] && ok "other user cannot delete media" || fail "foreign delete allowed"
[ "$(code -X DELETE -H "Authorization: Bearer $TAL" "$GW/media/$COVER")" = 204 ] && ok "owner deleted cover" || fail "owner delete failed"
[ "$(code "$GW/media/$COVER")" = 404 ] && ok "deleted media -> 404" || fail "deleted media still served"
[ "$(code -X DELETE -H "Authorization: Bearer $TAL" "$GW/media/$DOC")" = 409 ] && ok "evidence doc cannot be deleted while attached" || fail "evidence doc deleted"

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

step "organizer profile"
R=$(gql "$TAL" 'query{myOrganizerProfile{displayName}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "talent cannot read organizer profile" || fail "talent read organizer profile"
R=$(gql "$ORG" 'query{myOrganizerProfile{displayName organization homePlaceCode eventTypes{code}}}'); expect "new organizer profile is empty" "$R" '.data.myOrganizerProfile|[.organization,.homePlaceCode,(.eventTypes|length)]|tostring' '[null,null,0]'
R=$(gql "$ORG" 'mutation{updateMyOrganizerProfile(input:{organization:"E2E Events Ltd",phone:"+233 24 000 1111",homePlaceCode:"ashanti",eventTypeCodes:["wedding","funeral"]}){organization phone homePlaceCode homePlaceName eventTypes{code name}}}')
expect "profile saved" "$R" '.data.updateMyOrganizerProfile|[.organization,.homePlaceCode,(.eventTypes|map(.code)|sort|join(","))]|join("|")' 'E2E Events Ltd|ashanti|funeral,wedding'
R=$(gql "$ORG" 'mutation{updateMyOrganizerProfile(input:{organization:""}){organization homePlaceCode eventTypes{code}}}'); expect "partial update keeps region + events" "$R" '.data.updateMyOrganizerProfile|[.organization,.homePlaceCode,(.eventTypes|length)]|tostring' '[null,"ashanti",2]'
R=$(gql "$ORG" 'mutation{updateMyOrganizerProfile(input:{eventTypeCodes:["nope"]}){organization}}'); echo "$R" | jq -e '.errors' >/dev/null && ok "unknown event type rejected" || fail "unknown event type accepted"
R=$(gql "$ORG" 'query{myOrganizerProfile{homePlaceName eventTypes{code}}}'); expect "profile persisted" "$R" '.data.myOrganizerProfile.eventTypes|length' 2

step "forgot password (see scripts/e2e-reset.sh)"
E2E_NESTED=1 bash "$(dirname "$0")/e2e-reset.sh" "$GW" || FAILS=$((FAILS+1))

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
