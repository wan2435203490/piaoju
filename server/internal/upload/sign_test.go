package upload

// 签名 URL 与静态服务测试：verifySignedPath 表驱动（有效/过期/篡改/坏参）+
// Serve httptest 级测试（签名校验、safeRelPath 路径穿越、40401 信封）。
// /uploads/* 是全仓库唯一不带 Auth 的公开路由（cmd/api/router.go），签名即唯一访问控制，
// 此处回归意味着可越权读任意用户票据照片。

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

const signSecret = "sign-test-secret"

// flipHexDigit 篡改 hex 签名的第一个字符（保持仍是合法 hex，专测 HMAC 不匹配路径）。
func flipHexDigit(s string) string {
	if s[0] == '0' {
		return "1" + s[1:]
	}
	return "0" + s[1:]
}

func TestVerifySignedPath(t *testing.T) {
	now := time.Date(2026, 7, 13, 0, 0, 0, 0, time.UTC)
	const p = "/uploads/7/a.jpg"
	exp := now.Add(time.Hour).Unix()
	expStr := strconv.FormatInt(exp, 10)
	sig := signature(signSecret, p, exp)

	tests := []struct {
		name   string
		secret string
		path   string
		exp    string
		sig    string
		now    time.Time
		want   bool
	}{
		{"valid", signSecret, p, expStr, sig, now, true},
		{"valid at exact expiry second", signSecret, p, expStr, sig, time.Unix(exp, 0), true},
		{"expired", signSecret, p, expStr, sig, time.Unix(exp+1, 0), false},
		{"empty exp", signSecret, p, "", sig, now, false},
		{"non-numeric exp", signSecret, p, "16x9", sig, now, false},
		{"exp extended after signing", signSecret, p, strconv.FormatInt(exp+3600, 10), sig, now, false},
		{"missing sig", signSecret, p, expStr, "", now, false},
		{"non-hex sig", signSecret, p, expStr, "zz" + sig[2:], now, false},
		{"tampered sig", signSecret, p, expStr, flipHexDigit(sig), now, false},
		{"sig for another path", signSecret, "/uploads/8/a.jpg", expStr, sig, now, false},
		{"wrong secret", "other-secret", p, expStr, sig, now, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := verifySignedPath(tc.secret, tc.path, tc.exp, tc.sig, tc.now); got != tc.want {
				t.Fatalf("verifySignedPath = %v, want %v", got, tc.want)
			}
		})
	}
}

// SignURL 产出的 URL 必须能被 verifySignedPath 原样验证通过（生成与校验闭环）。
func TestSignURLRoundTrip(t *testing.T) {
	const p = "/uploads/7/a.jpg"
	u := SignURL(signSecret, p, URLTTL)
	path, query, ok := strings.Cut(u, "?")
	if !ok || path != p {
		t.Fatalf("SignURL = %q, want path %q + query", u, p)
	}
	params := map[string]string{}
	for _, kv := range strings.Split(query, "&") {
		k, v, _ := strings.Cut(kv, "=")
		params[k] = v
	}
	if !verifySignedPath(signSecret, path, params["exp"], params["sig"], time.Now()) {
		t.Fatalf("fresh SignURL %q failed verification", u)
	}
	if verifySignedPath(signSecret, path, params["exp"], params["sig"], time.Now().Add(URLTTL+time.Minute)) {
		t.Fatalf("SignURL %q still valid after TTL", u)
	}
}

// newServeFixture 造上传目录：dir/7/a.jpg 为合法文件，dir 外放一个 secret.txt 当穿越靶子。
func newServeFixture(t *testing.T) (dir string, fileBody string) {
	t.Helper()
	dir = t.TempDir()
	fileBody = "jpeg-bytes"
	if err := os.MkdirAll(filepath.Join(dir, "7"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "7", "a.jpg"), []byte(fileBody), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// dir 的父目录（t.TempDir 根，同样会被清理）：穿越一层即可命中。
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secret.txt"), []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}
	return dir, fileBody
}

// signedTarget 为任意（可能恶意的）解码后 path 生成合法签名的请求 target。
// encPath 是写进请求行的形式（NUL 等需百分号编码），为空则用 path 原样。
func signedTarget(path, encPath string, ttl time.Duration) string {
	if encPath == "" {
		encPath = path
	}
	exp := time.Now().Add(ttl).Unix()
	return encPath + "?exp=" + strconv.FormatInt(exp, 10) + "&sig=" + signature(signSecret, path, exp)
}

func TestServe(t *testing.T) {
	dir, fileBody := newServeFixture(t)
	h := Serve(dir, signSecret)

	const valid = "/uploads/7/a.jpg"
	tests := []struct {
		name   string
		target string
	}{
		{"expired signature", signedTarget(valid, "", -time.Minute)},
		{"tampered sig", func() string {
			u := signedTarget(valid, "", time.Hour)
			last := "a" // 改末位 hex（保证与原值不同且仍是合法 hex）
			if u[len(u)-1] == 'a' {
				last = "b"
			}
			return u[:len(u)-1] + last
		}()},
		{"missing exp and sig", valid},
		{"non-numeric exp", valid + "?exp=abc&sig=" + signature(signSecret, valid, 0)},
		{"traversal ../ validly signed", signedTarget("/uploads/../secret.txt", "", time.Hour)},
		{"traversal deep ../../ validly signed", signedTarget("/uploads/7/../../secret.txt", "", time.Hour)},
		{"backslash rejected", signedTarget(`/uploads/7\..\secret.txt`, "", time.Hour)},
		{"NUL byte rejected", signedTarget("/uploads/7/\x00a.jpg", "/uploads/7/%00a.jpg", time.Hour)},
		{"empty segment rejected", signedTarget("/uploads//7/a.jpg", "", time.Hour)},
		{"dot segment rejected", signedTarget("/uploads/./7/a.jpg", "", time.Hour)},
		{"directory not served", signedTarget("/uploads/7", "", time.Hour)},
		{"nonexistent file", signedTarget("/uploads/7/nope.jpg", "", time.Hour)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", rec.Code)
			}
			var env envelope
			if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v (body %q)", err, rec.Body.String())
			}
			if env.Code != 40401 {
				t.Fatalf("code = %d, want 40401", env.Code)
			}
			if strings.Contains(rec.Body.String(), "TOP-SECRET") || strings.Contains(rec.Body.String(), fileBody) {
				t.Fatalf("response leaked file content: %q", rec.Body.String())
			}
		})
	}

	t.Run("valid signature serves file", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(http.MethodGet, signedTarget(valid, "", time.Hour), nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d (body %q), want 200", rec.Code, rec.Body.String())
		}
		if rec.Body.String() != fileBody {
			t.Fatalf("body = %q, want %q", rec.Body.String(), fileBody)
		}
	})
}

// 与 cmd/api/router.go 一致的挂载方式（根路由、不带 Auth）走一遍真 HTTP，
// 验证 SignURL 产出的 URL 端到端可用、过期后 40401。
func TestServeMountedOnRootRouter(t *testing.T) {
	dir, fileBody := newServeFixture(t)
	r := chi.NewRouter()
	r.Get("/uploads/*", Serve(dir, signSecret))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	get := func(t *testing.T, u string) (int, []byte) {
		t.Helper()
		resp, err := http.Get(srv.URL + u)
		if err != nil {
			t.Fatalf("GET %s: %v", u, err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		return resp.StatusCode, body
	}

	status, body := get(t, SignURL(signSecret, "/uploads/7/a.jpg", URLTTL))
	if status != http.StatusOK || string(body) != fileBody {
		t.Fatalf("fresh signed URL: status/body = %d/%q, want 200/%q", status, body, fileBody)
	}

	status, body = get(t, SignURL(signSecret, "/uploads/7/a.jpg", -time.Minute))
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v (body %q)", err, body)
	}
	if status != http.StatusNotFound || env.Code != 40401 {
		t.Fatalf("expired signed URL: status/code = %d/%d, want 404/40401", status, env.Code)
	}
}
