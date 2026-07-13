#!/bin/bash
# Wave 4 冒烟：sync push/pull 端到端（含票↔交易联动、LWW stale、墓碑、游标）
set -u
BASE=http://localhost:8080/api/v1
EMAIL="sync-$(date +%s)@test.local"
TXID=$(uuidgen | tr 'A-Z' 'a-z')
TKID=$(uuidgen | tr 'A-Z' 'a-z')
TKTX=$(uuidgen | tr 'A-Z' 'a-z')
FAIL=0

step() { echo; echo "== $1"; }
check() {
  if echo "$2" | jq -e "$3" >/dev/null 2>&1; then echo "PASS: $1"
  else echo "FAIL: $1"; echo "$2" | jq -c . 2>/dev/null || echo "$2"; FAIL=1; fi
}

R=$(curl -s -X POST $BASE/auth/register -H 'Content-Type: application/json' \
  -d "{\"email\":\"$EMAIL\",\"password\":\"syncpass123\",\"nickname\":\"同步侠\"}")
AT=$(echo "$R" | jq -r .data.accessToken)
AUTH="Authorization: Bearer $AT"
CAT=$(curl -s $BASE/categories -H "$AUTH" | jq -r '[.data.items[]|select(.kind=="expense")][0].id')

step "1. push: 新建交易 + 新建票（客户端 transactionId）"
PUSH=$(cat <<EOF
{"changes":[
 {"entity":"transaction","op":"upsert","clientUpdatedAt":"2026-07-13T10:00:00Z",
  "payload":{"id":"$TXID","amountCents":1800,"direction":"expense","categoryId":$CAT,
             "note":"离线奶茶","occurredAt":"2026-07-13T08:30:00Z","paymentMethod":"wechat"}},
 {"entity":"ticket","op":"upsert","clientUpdatedAt":"2026-07-13T10:00:00Z",
  "payload":{"id":"$TKID","transactionId":"$TKTX","kind":"movie","title":"离线建的电影票",
             "venue":"CGV","eventTime":"2026-07-12T19:30:00Z","seat":"7排8座",
             "extra":{"cinema":"CGV","hall":"IMAX","filmFormat":"IMAX"},"rating":5,"memo":"",
             "amountCents":4500,"categoryId":$CAT,"paymentMethod":"alipay",
             "occurredAt":"2026-07-12T19:00:00Z","attachmentIds":[]}}
]}
EOF
)
R=$(curl -s -X POST $BASE/sync/push -H "$AUTH" -H 'Content-Type: application/json' -d "$PUSH")
check "两条都 applied" "$R" '.code==0 and ([.data.results[]|select(.status=="applied")]|length==2)'

step "2. 票的联动交易用了客户端传的 transactionId（离线账本一致性的关键）"
R=$(curl -s $BASE/tickets/$TKID -H "$AUTH")
check "ticket.transaction.id == 客户端 transactionId" "$R" ".code==0 and .data.transaction.id==\"$TKTX\""
R=$(curl -s "$BASE/transactions?month=2026-07" -H "$AUTH")
check "账本里有这笔联动交易(该 id)" "$R" ".code==0 and ([.data.items[]|select(.id==\"$TKTX\")]|length==1)"

step "3. pull: 拉回全部（含分类）"
R=$(curl -s "$BASE/sync/pull?since=&limit=200" -H "$AUTH")
check "pull 回 2 交易 + 1 票 + 分类" "$R" '.code==0 and (.data.transactions|length==2) and (.data.tickets|length==1) and (.data.categories|length>=11)'
CURSOR=$(echo "$R" | jq -r .data.nextCursor)
check "nextCursor 非空" "$R" '.data.nextCursor|length>0'

step "4. LWW: 用更旧的 clientUpdatedAt 重推 → stale"
STALE="{\"changes\":[{\"entity\":\"transaction\",\"op\":\"upsert\",\"clientUpdatedAt\":\"2020-01-01T00:00:00Z\",\"payload\":{\"id\":\"$TXID\",\"amountCents\":9999,\"direction\":\"expense\",\"categoryId\":$CAT,\"note\":\"旧改动\",\"occurredAt\":\"2026-07-13T08:30:00Z\",\"paymentMethod\":\"cash\"}}]}"
R=$(curl -s -X POST $BASE/sync/push -H "$AUTH" -H 'Content-Type: application/json' -d "$STALE")
check "判 stale 不覆盖" "$R" '.code==0 and .data.results[0].status=="stale"'
R=$(curl -s "$BASE/transactions?month=2026-07" -H "$AUTH")
check "金额仍是 1800（未被旧值覆盖）" "$R" ".code==0 and ([.data.items[]|select(.id==\"$TXID\")][0].amountCents==1800)"

step "5. 增量 pull：游标之后无新数据"
R=$(curl -s "$BASE/sync/pull?since=$CURSOR&limit=200" -H "$AUTH")
check "无新增（游标水位有效）" "$R" '.code==0 and (.data.transactions|length==0) and (.data.tickets|length==0)'

step "6. push delete → 墓碑随 pull 下发"
# clientUpdatedAt 必须是「当前时间 + 毫秒精度」（契约 §8）：服务端 updated_at 是
# DATETIME(3)，秒级 .000 恒早于同秒内的服务端版本 → 会被 LWW 误判 stale。
NOW=$(python3 -c "import datetime;print(datetime.datetime.now(datetime.UTC).strftime('%Y-%m-%dT%H:%M:%S.%f')[:-3]+'Z')")
DEL="{\"changes\":[{\"entity\":\"ticket\",\"op\":\"delete\",\"clientUpdatedAt\":\"$NOW\",\"payload\":{\"id\":\"$TKID\"}}]}"
R=$(curl -s -X POST $BASE/sync/push -H "$AUTH" -H 'Content-Type: application/json' -d "$DEL")
check "delete applied" "$R" '.code==0 and .data.results[0].status=="applied"'
R=$(curl -s "$BASE/sync/pull?since=$CURSOR&limit=200" -H "$AUTH")
check "票墓碑 deletedAt 非空" "$R" ".code==0 and ([.data.tickets[]|select(.id==\"$TKID\" and .deletedAt!=null)]|length==1)"
check "联动交易也立墓碑" "$R" ".code==0 and ([.data.transactions[]|select(.id==\"$TKTX\" and .deletedAt!=null)]|length==1)"

echo
[ $FAIL -eq 0 ] && echo "SYNC SMOKE ALL GREEN ✅" || echo "SYNC SMOKE HAS FAILURES ❌"
exit $FAIL
