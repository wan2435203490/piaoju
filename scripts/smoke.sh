#!/bin/bash
# T3.1 全栈冒烟：注册→登录→记账(幂等)→建票→详情→统计→删票
set -u
BASE=http://localhost:8080/api/v1
EMAIL="smoke-$(date +%s)@test.local"
PASS="smokepass123"
TXID=$(uuidgen | tr 'A-Z' 'a-z')
TKID=$(uuidgen | tr 'A-Z' 'a-z')
TKTXID=$(uuidgen | tr 'A-Z' 'a-z')
FAIL=0

step() { echo; echo "== $1"; }
check() { # $1 desc, $2 json, $3 jq-expr expected truthy
  if echo "$2" | jq -e "$3" >/dev/null 2>&1; then
    echo "PASS: $1"
  else
    echo "FAIL: $1"; echo "$2" | jq -c . 2>/dev/null || echo "$2"; FAIL=1
  fi
}

step "1. register"
R=$(curl -s -X POST $BASE/auth/register -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\",\"nickname\":\"冒烟侠\"}")
check "register code=0 + tokens" "$R" '.code==0 and (.data.accessToken|length>0)'
AT=$(echo "$R" | jq -r .data.accessToken)

step "2. login"
R=$(curl -s -X POST $BASE/auth/login -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"$PASS\"}")
check "login code=0" "$R" '.code==0 and (.data.accessToken|length>0)'
AT=$(echo "$R" | jq -r .data.accessToken)
AUTH="Authorization: Bearer $AT"

step "3. categories seed"
R=$(curl -s $BASE/categories -H "$AUTH")
check "categories >=11" "$R" '.code==0 and (.data.items|length>=11)'
CAT_EXP=$(echo "$R" | jq -r '[.data.items[]|select(.kind=="expense")][0].id')
CAT_ENT=$(echo "$R" | jq -r '[.data.items[]|select(.name=="娱乐")][0].id // empty')
[ -z "$CAT_ENT" ] && CAT_ENT=$CAT_EXP

step "4. create transaction (奶茶 18.00)"
TX="{\"id\":\"$TXID\",\"amountCents\":1800,\"direction\":\"expense\",\"categoryId\":$CAT_EXP,\"note\":\"冒烟奶茶\",\"occurredAt\":\"2026-07-13T08:30:00Z\",\"paymentMethod\":\"wechat\"}"
R=$(curl -s -X POST $BASE/transactions -H "$AUTH" -H 'Content-Type: application/json' -d "$TX")
check "tx create code=0 id echo" "$R" ".code==0 and .data.id==\"$TXID\""

step "5. idempotent replay (same id)"
R=$(curl -s -X POST $BASE/transactions -H "$AUTH" -H 'Content-Type: application/json' -d "$TX")
check "tx replay code=0" "$R" '.code==0'
R=$(curl -s "$BASE/transactions?month=2026-07" -H "$AUTH")
check "tx list exactly 1 (no dup)" "$R" '.code==0 and (.data.items|length==1)'

step "6. create ticket (电影票 45.00, 联动建交易)"
TK="{\"id\":\"$TKID\",\"transactionId\":\"$TKTXID\",\"kind\":\"movie\",\"title\":\"冒烟测试:星际穿越\",\"venue\":\"CGV影城\",\"eventTime\":\"2026-07-12T19:30:00Z\",\"seat\":\"7排8座\",\"extra\":{\"cinema\":\"CGV影城\",\"hall\":\"IMAX厅\",\"filmFormat\":\"IMAX\"},\"rating\":5,\"memo\":\"\",\"amountCents\":4500,\"categoryId\":$CAT_ENT,\"paymentMethod\":\"alipay\",\"occurredAt\":\"2026-07-12T19:00:00Z\",\"attachmentIds\":[]}"
R=$(curl -s -X POST $BASE/tickets -H "$AUTH" -H 'Content-Type: application/json' -d "$TK")
check "ticket create + embedded tx" "$R" '.code==0 and .data.transaction.amountCents==4500'

step "7. ticket detail"
R=$(curl -s $BASE/tickets/$TKID -H "$AUTH")
check "detail kind/extra" "$R" '.code==0 and .data.kind=="movie" and .data.extra.filmFormat=="IMAX"'

step "8. tx list now 2 (ticket-linked included)"
R=$(curl -s "$BASE/transactions?month=2026-07" -H "$AUTH")
check "2 txs, one ticketId set" "$R" ".code==0 and (.data.items|length==2) and ([.data.items[]|select(.ticketId==\"$TKID\")]|length==1)"

step "9. stats monthly"
R=$(curl -s "$BASE/stats/monthly?month=2026-07" -H "$AUTH")
check "expense=6300 (1800+4500)" "$R" '.code==0 and .data.expenseCents==6300'

step "10. stats tickets"
R=$(curl -s "$BASE/stats/tickets?year=2026" -H "$AUTH")
check "total=1 movie 4500" "$R" '.code==0 and .data.total==1 and (.data.byKind[]|select(.kind=="movie")|.cents==4500)'

step "11. delete ticket-linked tx directly → 40001"
R=$(curl -s -X DELETE $BASE/transactions/$(curl -s "$BASE/transactions?month=2026-07" -H "$AUTH" | jq -r ".data.items[]|select(.ticketId==\"$TKID\")|.id") -H "$AUTH")
check "blocked with 40001" "$R" '.code==40001'

step "12. delete ticket → cascades soft-del tx"
R=$(curl -s -X DELETE $BASE/tickets/$TKID -H "$AUTH")
check "ticket delete code=0" "$R" '.code==0'
R=$(curl -s "$BASE/transactions?month=2026-07" -H "$AUTH")
check "tx list back to 1" "$R" '.code==0 and (.data.items|length==1)'
R=$(curl -s "$BASE/stats/monthly?month=2026-07" -H "$AUTH")
check "stats expense back to 1800" "$R" '.code==0 and .data.expenseCents==1800'

step "13. unauth access → 401xx"
R=$(curl -s $BASE/transactions)
check "no token rejected" "$R" '.code>=40100 and .code<40200'

echo
[ $FAIL -eq 0 ] && echo "SMOKE ALL GREEN ✅" || echo "SMOKE HAS FAILURES ❌"
exit $FAIL
