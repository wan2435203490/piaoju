package ticket

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// keyset 游标（GET /tickets 按 event_time DESC, id DESC 翻页）：
// base64url("<eventTimeUnixMilli>:<ticketID>")，对客户端不透明（conventions §2）。
// 用 (event_time, id) 双键断点，event_time 相同的票不会漏页/重页。

func encodeCursor(eventTime time.Time, id string) string {
	raw := strconv.FormatInt(eventTime.UnixMilli(), 10) + ":" + id
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
