package transaction

import (
	"errors"
	"testing"
	"time"

	"piaoju/internal/platform/apperr"
)

// TestCursorRoundTrip 游标稳定性：encode → decode 必须还原毫秒级时间与 id。
func TestCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 7, 12, 11, 30, 0, 123e6, time.UTC)
	id := "e1b041cd-b29c-4f45-a48c-9294b6a9bf8f"

	c := encodeCursor(at, id)
	gotTime, gotID, err := decodeCursor(c)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if !gotTime.Equal(at) {
		t.Fatalf("time = %v, want %v", gotTime, at)
	}
	if gotID != id {
		t.Fatalf("id = %q, want %q", gotID, id)
	}
	// 同输入重复编码结果一致（对客户端可缓存重放）。
	if encodeCursor(at, id) != c {
		t.Fatal("encodeCursor not deterministic")
	}
}

// TestCursorRejectsGarbage 非法游标一律 40001，不 panic 不泄内部格式。
func TestCursorRejectsGarbage(t *testing.T) {
	for _, s := range []string{
		"!!!not-base64!!!",
		"aGVsbG8",    // base64("hello")，无冒号
		"MTIzOmFiYw", // "123:abc"，id 非 UUID
		"eHg6YWJjZA", // "xx:abcd"，毫秒非数字
	} {
		_, _, err := decodeCursor(s)
		var ae *apperr.Error
		if !errors.As(err, &ae) || ae.Code != apperr.CodeInvalidParam {
			t.Fatalf("decodeCursor(%q) err = %v, want 40001", s, err)
		}
	}
}
