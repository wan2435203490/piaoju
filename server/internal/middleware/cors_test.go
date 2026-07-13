package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func corsReq(method, origin string, preflight bool) *httptest.ResponseRecorder {
	h := CORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(method, "/api/v1/ping", nil)
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	if preflight {
		req.Header.Set("Access-Control-Request-Method", "POST")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestCORSAllowsLocalhostAnyPort(t *testing.T) {
	for _, origin := range []string{
		"http://localhost:5173",
		"http://localhost:4173",
		"http://127.0.0.1:8081",
		"https://localhost",
		"capacitor://localhost",
	} {
		rec := corsReq(http.MethodGet, origin, false)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("origin %s: ACAO = %q, want echoed", origin, got)
		}
	}
}

func TestCORSRejectsForeignOrigin(t *testing.T) {
	for _, origin := range []string{
		"https://evil.example.com",
		"http://localhost.evil.com",
		"http://mylocalhost:5173",
	} {
		rec := corsReq(http.MethodGet, origin, false)
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("origin %s: ACAO = %q, want empty", origin, got)
		}
	}
}

func TestCORSPreflightShortCircuits(t *testing.T) {
	rec := corsReq(http.MethodOptions, "http://localhost:5173", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatal("missing Access-Control-Allow-Methods on preflight")
	}
	if rec.Header().Get("Access-Control-Allow-Headers") == "" {
		t.Fatal("missing Access-Control-Allow-Headers on preflight")
	}
}

func TestCORSNoOriginPassesThrough(t *testing.T) {
	rec := corsReq(http.MethodGet, "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("ACAO must be absent for same-origin request")
	}
}
