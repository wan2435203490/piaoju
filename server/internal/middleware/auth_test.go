package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"piaoju/internal/platform/token"
)

func newAuthedServer(t *testing.T, tm *token.Manager) (http.Handler, *int64) {
	t.Helper()
	var gotUID int64
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUID = UID(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	return Auth(tm)(inner), &gotUID
}

func doReq(t *testing.T, h http.Handler, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func assert40101(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	var env struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("bad envelope: %v", err)
	}
	if env.Code != 40101 {
		t.Fatalf("code = %d, want 40101", env.Code)
	}
	if string(env.Data) != "null" {
		t.Fatalf("data = %s, want null", env.Data)
	}
}

func TestAuthMissingHeader(t *testing.T) {
	tm := token.NewManager("secret")
	h, gotUID := newAuthedServer(t, tm)
	assert40101(t, doReq(t, h, ""))
	if *gotUID != 0 {
		t.Fatal("inner handler must not run without token")
	}
}

func TestAuthMalformedHeader(t *testing.T) {
	tm := token.NewManager("secret")
	h, _ := newAuthedServer(t, tm)
	for _, a := range []string{"Bearer", "Bearer ", "Basic dXNlcg==", "bogus"} {
		assert40101(t, doReq(t, h, a))
	}
}

func TestAuthFakeToken(t *testing.T) {
	tm := token.NewManager("secret")
	h, _ := newAuthedServer(t, tm)
	assert40101(t, doReq(t, h, "Bearer this.is.forged"))
}

func TestAuthWrongSecretToken(t *testing.T) {
	other := token.NewManager("other-secret")
	s, err := other.Sign(9, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := newAuthedServer(t, token.NewManager("secret"))
	assert40101(t, doReq(t, h, "Bearer "+s))
}

func TestAuthExpiredToken(t *testing.T) {
	tm := token.NewManager("secret")
	s, err := tm.Sign(9, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := newAuthedServer(t, tm)
	rec := doReq(t, h, "Bearer "+s)
	assert40101(t, rec)
}

func TestAuthValidTokenInjectsUID(t *testing.T) {
	tm := token.NewManager("secret")
	s, err := tm.Sign(1234, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h, gotUID := newAuthedServer(t, tm)
	rec := doReq(t, h, "Bearer "+s)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body)
	}
	if *gotUID != 1234 {
		t.Fatalf("UID(ctx) = %d, want 1234", *gotUID)
	}
}

// TestAuthSchemeCaseInsensitive RFC 6750 scheme 大小写不敏感。
func TestAuthSchemeCaseInsensitive(t *testing.T) {
	tm := token.NewManager("secret")
	s, err := tm.Sign(5, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	h, gotUID := newAuthedServer(t, tm)
	rec := doReq(t, h, "bearer "+s)
	if rec.Code != http.StatusOK || *gotUID != 5 {
		t.Fatalf("lowercase bearer rejected: status=%d uid=%d", rec.Code, *gotUID)
	}
}

func TestUIDAbsent(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if uid := UID(req.Context()); uid != 0 {
		t.Fatalf("UID on bare context = %d, want 0", uid)
	}
}
