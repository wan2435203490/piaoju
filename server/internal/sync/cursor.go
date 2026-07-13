package sync

import (
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// 服务端单调游标（GET /sync/pull）：base64url("<updatedAtUnixMilli>:<entityID>")，
// 对客户端不透明（conventions §2）。编码方案与 transaction/ticket 的 keyset 游标一致。
//
// 语义：transactions 与 tickets 各自按 (updated_at, id) 升序 keyset 扫描，服务端把两侧
// 结果按同一 (updated_at, id) 全序归并后截断到 limit——游标即「全序上已下发到的断点」，
// 对两张表施加同一断点条件即可无重无漏地续页。禁止 offset 分页（写入会导致行漂移丢数据）。
//
// updated_at 由服务端时钟写入（push 不采信客户端时间戳做 updated_at），保证游标单调。

func encodeCursor(updatedAt time.Time, id string) string {
	raw := strconv.FormatInt(updatedAt.UnixMilli(), 10) + ":" + id
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

// beforeKey 报告 (aT,aID) 在全序上是否早于 (bT,bID)（归并排序用）。
func beforeKey(aT time.Time, aID string, bT time.Time, bID string) bool {
	if !aT.Equal(bT) {
		return aT.Before(bT)
	}
	return aID < bID
}
