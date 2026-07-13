package transaction

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// keyset 游标（GET /transactions 按 occurred_at DESC, id DESC 翻页）：
// base64url("<occurredAtUnixMilli>:<transactionID>")，对客户端不透明（conventions §2）。
// 编码方案与 ticket/cursor.go 保持一致；用 (occurred_at, id) 双键断点，
// occurred_at 相同的交易不会漏页/重页。

func encodeCursor(occurredAt time.Time, id string) string {
	raw := strconv.FormatInt(occurredAt.UnixMilli(), 10) + ":" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", badParam("invalid cursor")
	}
	ms, id, ok := strings.Cut(string(raw), ":")
	if !ok {
		return time.Time{}, "", badParam("invalid cursor")
	}
	n, err := strconv.ParseInt(ms, 10, 64)
	if err != nil || !uuidRe.MatchString(id) {
		return time.Time{}, "", badParam("invalid cursor")
	}
	return time.UnixMilli(n).UTC(), id, nil
}
