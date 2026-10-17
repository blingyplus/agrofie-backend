#!/usr/bin/env bash
# End-to-end test of "Forgot password" (Kratos recovery, code method) against the
# local stack: register a temp user (@e2e.test) -> requestPasswordReset -> read the
# 6-digit code from MailHog's API -> resetPassword -> login with the new password
# works, the old one and the old session do not. Local dev only (MailHog on :8025).
# Usage: scripts/e2e-reset.sh [gateway-url]   (also run from scripts/e2e.sh)
set -uo pipefail
GW="${1:-${GATEWAY_URL:-http://localhost:8080}}"
MAILHOG="${MAILHOG_URL:-http://localhost:8025}"
RUN="$(date +%s)$RANDOM"
EMAIL="reset$RUN@e2e.test"
OLD="Old-e2e-pw-$RANDOM-x"
NEW="New-e2e-pw-$RANDOM-y"
FAILS=0

gql() { # gql TOKEN QUERY [VARS_JSON]
  local tok="$1" q="$2" v="${3:-}"; [ -n "$v" ] || v='{}'
  jq -n --arg q "$q" --argjson v "$v" '{query:$q,variables:$v}' |
    curl -sS "$GW/graphql" -H 'content-type: application/json' ${tok:+-H "Authorization: Bearer $tok"} -d @-
}
ok()   { echo "  ok   $1"; }
fail() { echo "  FAIL $1"; FAILS=$((FAILS+1)); }
expect() { local got; got=$(echo "$2" | jq -r "$3" 2>/dev/null); if [ "$got" = "$4" ]; then ok "$1 ($got)"; else fail "$1: wanted '$4' got '$got'  $(echo "$2" | head -c 300)"; fi; }
errmsg() { echo "$1" | jq -r '.errors[0].message // empty'; }
mails_for() { curl -s "$MAILHOG/api/v2/search?kind=to&query=$1" | jq -r '.total // 0'; }
# Latest 6-digit code mailed to $1 (own line in the body; MIME boundaries also contain digits).
latest_code() {
  local i c
  for i in $(seq 1 20); do
    c=$(curl -s "$MAILHOG/api/v2/search?kind=to&query=$1" | jq -r '.items[0].Content.Body // empty' | tr -d '\r' | grep -E '^[0-9]{6}$' | head -1)
    [ -n "$c" ] && { echo "$c"; return; }
    sleep 0.5
  done
}
# Standalone runs clean up after themselves; when scripts/e2e.sh runs us (E2E_NESTED=1)
# it does the cleanup once at the end, so we do not delete its users mid-run.
[ -n "${E2E_NESTED:-}" ] || trap '"$(dirname "$0")/e2e-cleanup.sh"' EXIT

echo "== forgot password (Kratos recovery)"
R=$(gql "" 'mutation($i:RegisterInput!){register(input:$i){sessionToken}}' "{\"i\":{\"email\":\"$EMAIL\",\"password\":\"$OLD\",\"displayName\":\"E2E Reset\",\"roleCode\":\"organizer\"}}")
OLDTOK=$(echo "$R" | jq -r .data.register.sessionToken); expect "temp user registered" "$R" '.data.register.sessionToken|startswith("ory_st_")' true

R=$(gql "" 'mutation{requestPasswordReset(email:"not-an-email")}'); [ -n "$(errmsg "$R")" ] && ok "malformed email rejected" || fail "malformed email accepted"
R=$(gql "" 'mutation($e:String!){requestPasswordReset(email:$e)}' "{\"e\":\"nobody$RUN@e2e.test\"}"); expect "unknown email still returns true (no enumeration)" "$R" '.data.requestPasswordReset' true
R=$(gql "" 'mutation($e:String!){requestPasswordReset(email:$e)}' "{\"e\":\"$EMAIL\"}"); expect "request for real account returns true" "$R" '.data.requestPasswordReset' true

CODE=$(latest_code "$EMAIL")
[[ "$CODE" =~ ^[0-9]{6}$ ]] && ok "6-digit code arrived in MailHog" || fail "no code in MailHog for $EMAIL"

R=$(gql "" 'mutation($e:String!){requestPasswordReset(email:$e)}' "{\"e\":\"$EMAIL\"}"); expect "immediate re-request returns true" "$R" '.data.requestPasswordReset' true
sleep 1; [ "$(mails_for "$EMAIL")" = 1 ] && ok "throttled: no second email within 30s" || fail "second email was sent ($(mails_for "$EMAIL"))"

WRONG=$([ "$CODE" = 000000 ] && echo 111111 || echo 000000)
R=$(gql "" 'mutation($e:String!,$c:String!,$p:String!){resetPassword(email:$e,code:$c,newPassword:$p)}' "{\"e\":\"$EMAIL\",\"c\":\"$WRONG\",\"p\":\"$NEW\"}")
[[ "$(errmsg "$R")" == *"wrong or has expired"* ]] && ok "wrong code rejected: $(errmsg "$R")" || fail "wrong code not rejected: $R"
R=$(gql "" 'mutation($e:String!,$c:String!){resetPassword(email:$e,code:$c,newPassword:"short")}' "{\"e\":\"$EMAIL\",\"c\":\"$CODE\"}")
[[ "$(errmsg "$R")" == *"at least 8"* ]] && ok "weak password rejected: $(errmsg "$R")" || fail "weak password not rejected: $R"

R=$(gql "" 'mutation($e:String!,$c:String!,$p:String!){resetPassword(email:$e,code:$c,newPassword:$p)}' "{\"e\":\"$EMAIL\",\"c\":\"$CODE\",\"p\":\"$NEW\"}")
expect "resetPassword with the emailed code" "$R" '.data.resetPassword' true

R=$(gql "" 'mutation($i:LoginInput!){login(input:$i){sessionToken}}' "{\"i\":{\"identifier\":\"$EMAIL\",\"password\":\"$NEW\"}}")
NEWTOK=$(echo "$R" | jq -r .data.login.sessionToken); expect "login with NEW password works" "$R" '.data.login.sessionToken|startswith("ory_st_")' true
R=$(gql "" 'mutation($i:LoginInput!){login(input:$i){sessionToken}}' "{\"i\":{\"identifier\":\"$EMAIL\",\"password\":\"$OLD\"}}"); [ -n "$(errmsg "$R")" ] && ok "login with OLD password fails" || fail "old password still works"
R=$(gql "$OLDTOK" 'query{me{id}}'); [ -n "$(errmsg "$R")" ] && ok "old session was invalidated" || fail "old session still valid"
R=$(gql "$NEWTOK" 'query{me{id}}'); expect "new session valid" "$R" '.data.me.id|length>10' true
R=$(gql "" 'mutation($e:String!,$c:String!,$p:String!){resetPassword(email:$e,code:$c,newPassword:$p)}' "{\"e\":\"$EMAIL\",\"c\":\"$CODE\",\"p\":\"Third-e2e-pw-$RANDOM\"}"); [ -n "$(errmsg "$R")" ] && ok "code cannot be reused" || fail "code reused"
R=$(gql "" 'mutation($e:String!){resetPassword(email:$e,code:"123456",newPassword:"Some-e2e-pw-1234")}' "{\"e\":\"nobody$RUN@e2e.test\"}"); [[ "$(errmsg "$R")" == *"wrong or has expired"* ]] && ok "unknown email gets the same wrong-code error" || fail "unknown email reset: $R"

echo
if [ "$FAILS" -eq 0 ]; then echo "RESET E2E PASSED"; else echo "RESET E2E: $FAILS FAILURE(S)"; exit 1; fi
