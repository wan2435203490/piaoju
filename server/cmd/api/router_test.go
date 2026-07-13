package main

// 路由骨架集成测试：healthz 信封逐字节对契约、/api/v1 Auth 链路端到端。
// 不碰 DB（newRouter 的 conn 仅透传给未来模块，骨架阶段传 nil 安全）。

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"piaoju/internal/platform/token"
)

func newTestServer(t *testing.T) (*httptest.Server, *token.Manager) {
	t.Helper()
	tm := token.NewManager("router-test-secret")
	srv := httptest.NewServer(newRouter(nil, tm))
	t.Cleanup(srv.Close)
	return srv, tm
}

func TestHealthzExactEnvelope(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	got := strings.TrimSpace(string(body))
	want := `{"code":0,"message":"ok","data":{"status":"up"}}`
	if got != want {
		t.Fatalf("healthz body = %s, want %s", got, want)
	}
}

func TestPingRequiresAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	resp, err := http.Get(srv.URL + "/api/v1/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	var env struct {
		Code int `json:"code"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.Code != 40101 {
		t.Fatalf("code = %d, want 40101", env.Code)
	}
}

func TestPingWithValidToken(t *testing.T) {
	srv, tm := newTestServer(t)
	access, err := tm.Sign(77, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/ping", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var env struct {
		Code int `json:"code"`
		Data struct {
			Pong bool  `json:"pong"`
			UID  int64 `json:"uid"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.Code != 0 || !env.Data.Pong || env.Data.UID != 77 {
		t.Fatalf("envelope = %+v, want code 0, pong true, uid 77", env)
	}
}

// TestCORSPreflightOnAuthedRoute 预检必须在 Auth 之前被 CORS 短路（浏览器预检不带 token）。
func TestCORSPreflightOnAuthedRoute(t *testing.T) {
	srv, _ := newTestServer(t)
	req, _ := http.NewRequest(http.MethodOptions, srv.URL+"/api/v1/ping", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	req.Header.Set("Access-Control-Request-Method", "GET")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatal("preflight missing ACAO for localhost dev origin")
	}
}
