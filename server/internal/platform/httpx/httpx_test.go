package httpx

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"piaoju/internal/platform/apperr"
)

// decode 解析响应体信封；data 保持 raw 供各用例细查。
func decode(t *testing.T, body string) (code int, message string, data json.RawMessage) {
	t.Helper()
	var env struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("body is not a valid envelope: %v\nbody: %s", err, body)
	}
	return env.Code, env.Message, env.Data
}

func TestOK(t *testing.T) {
	rec := httptest.NewRecorder()
	OK(rec, map[string]string{"status": "up"})

	if rec.Code != 200 {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type = %q", ct)
	}
	code, msg, data := decode(t, rec.Body.String())
	if code != 0 || msg != "ok" {
		t.Fatalf("envelope = (%d, %q), want (0, ok)", code, msg)
	}
	if string(data) != `{"status":"up"}` {
		t.Fatalf("data = %s", data)
	}
}

func TestOKNilData(t *testing.T) {
	rec := httptest.NewRecorder()
	OK(rec, nil)
	_, _, data := decode(t, rec.Body.String())
	if string(data) != "null" {
		t.Fatalf("data = %s, want null", data)
	}
}

func TestErrAppErr(t *testing.T) {
	rec := httptest.NewRecorder()
	Err(rec, apperr.New(apperr.CodeTokenExpired, "token expired"))

	if rec.Code != 401 {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	code, msg, data := decode(t, rec.Body.String())
	if code != 40101 || msg != "token expired" {
		t.Fatalf("envelope = (%d, %q), want (40101, token expired)", code, msg)
	}
	if string(data) != "null" {
		t.Fatalf("error data = %s, want null", data)
	}
}

func TestErrWrappedAppErr(t *testing.T) {
	rec := httptest.NewRecorder()
	Err(rec, fmt.Errorf("handler: %w", apperr.New(apperr.CodeNotFound, "ticket not found")))

	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	code, _, _ := decode(t, rec.Body.String())
	if code != 40401 {
		t.Fatalf("code = %d, want 40401", code)
	}
}

// TestErrUnknownNoLeak 未知错误必须转 50000 且不把内部细节写给客户端。
func TestErrUnknownNoLeak(t *testing.T) {
	rec := httptest.NewRecorder()
	secret := "dsn=root:hunter2@tcp(10.0.0.1)/prod"
	Err(rec, errors.New(secret))

	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	code, msg, data := decode(t, rec.Body.String())
	if code != 50000 {
		t.Fatalf("code = %d, want 50000", code)
	}
	if msg != "internal server error" {
		t.Fatalf("message = %q, want generic message", msg)
	}
	if string(data) != "null" {
		t.Fatalf("data = %s, want null", data)
	}
	if strings.Contains(rec.Body.String(), "hunter2") {
		t.Fatal("internal error detail leaked to client")
	}
}
