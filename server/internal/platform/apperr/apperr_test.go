package apperr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	e := New(CodeInvalidParam, "amountCents must be positive")
	if e.Code != 40001 {
		t.Fatalf("Code = %d, want 40001", e.Code)
	}
	if e.Msg != "amountCents must be positive" {
		t.Fatalf("Msg = %q", e.Msg)
	}
	if !strings.Contains(e.Error(), "40001") || !strings.Contains(e.Error(), e.Msg) {
		t.Fatalf("Error() = %q, want code and msg included", e.Error())
	}
}

func TestErrorsAsThroughWrap(t *testing.T) {
	wrapped := fmt.Errorf("service: create tx: %w", New(CodeConflict, "stale update"))
	var ae *Error
	if !errors.As(wrapped, &ae) {
		t.Fatal("errors.As failed to unwrap *apperr.Error")
	}
	if ae.Code != CodeConflict {
		t.Fatalf("Code = %d, want %d", ae.Code, CodeConflict)
	}
}

func TestHTTPStatus(t *testing.T) {
	cases := []struct {
		code int
		want int
	}{
		{CodeInvalidParam, 400},    // 40001
		{CodeUnsupportedEnum, 400}, // 40002
		{CodeTokenExpired, 401},    // 40101
		{CodeRefreshInvalid, 401},  // 40102
		{CodeBadCredentials, 401},  // 40103
		{CodeNotFound, 404},        // 40401
		{CodeEmailTaken, 409},      // 40901
		{CodeConflict, 409},        // 40902
		{CodeUploadTooLarge, 413},  // 41301
		{CodeInternal, 500},        // 50000
		{7, 500},                   // 非法段位兜底 500
	}
	for _, c := range cases {
		if got := New(c.code, "x").HTTPStatus(); got != c.want {
			t.Errorf("HTTPStatus(%d) = %d, want %d", c.code, got, c.want)
		}
	}
}
