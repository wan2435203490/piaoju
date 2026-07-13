package ticket

import (
	"encoding/base64"
	"testing"
	"time"

	"piaoju/internal/platform/apperr"
)

func TestCursorRoundTrip(t *testing.T) {
	et := time.Date(2026, 7, 10, 12, 30, 0, 123_000_000, time.UTC) // 毫秒精度（DATETIME(3)）
	c := encodeCursor(et, tkID)

	gotT, gotID, err := decodeCursor(c)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if !gotT.Equal(et) || gotID != tkID {
		t.Fatalf("got (%v, %s), want (%v, %s)", gotT, gotID, et, tkID)
	}
}

func TestDecodeCursorRejects(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"not base64url", "!!!"},
		{"no separator", base64.RawURLEncoding.EncodeToString([]byte("12345"))},
		{"non-numeric millis", base64.RawURLEncoding.EncodeToString([]byte("abc:" + tkID))},
		{"bad uuid", base64.RawURLEncoding.EncodeToString([]byte("1752148200000:not-a-uuid"))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := decodeCursor(tc.in)
			wantCode(t, err, apperr.CodeInvalidParam)
		})
	}
}
