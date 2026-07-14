package vision

// handler 层测试：真 HTTP（chi + middleware.Auth + JWT）+ sqlmock + fake LLM。
// 同时验证交付说明里的挂载方式与 ticket.Routes 共存（/tickets/recognize 静态路由
// 必须优先于 /tickets/{id} 通配匹配）。

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/token"
	"piaoju/internal/ticket"
)

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

// newTestServer 按交付说明的挂载方式组装路由（ticket + vision 同时挂在 /tickets 下）。
func newTestServer(t *testing.T, llm recognizer) (*httptest.Server, sqlmock.Sqlmock, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "7"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "7", "abc.jpg"), []byte("\xff\xd8\xff jpeg"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	tm := token.NewManager("vision-test-secret")
	access, err := tm.Sign(uidA, time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))
			sec.Mount("/tickets", ticket.Routes(db, "upload-test-secret"))
			sec.Mount("/tickets/recognize", routes(&service{db: db, dir: dir, llm: llm}))
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, mock, access
}

func post(t *testing.T, srv *httptest.Server, path, body, access string) (int, envelope) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("body is not an envelope: %v\nbody: %s", err, raw)
	}
	return resp.StatusCode, env
}

// 正常识别：200 + code 0 + data 为草稿；路由不被 /tickets/{id} 抢走。
func TestHTTPRecognizeOK(t *testing.T) {
	srv, mock, access := newTestServer(t, &fakeLLM{out: goodOutput()})
	expectAttachment(mock, "7/abc.jpg")

	status, env := post(t, srv, "/api/v1/tickets/recognize", `{"attachmentId":42}`, access)
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("status=%d env=%+v", status, env)
	}
	var d Draft
	if err := json.Unmarshal(env.Data, &d); err != nil {
		t.Fatalf("data: %v", err)
	}
	if d.Kind != "movie" || d.AmountCents != 6850 || d.Extra["filmFormat"] != "IMAX" {
		t.Fatalf("draft = %+v", d)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// 未配置 key → 500 信封 50001（服务照常起、其余接口不受影响）。
func TestHTTPRecognizeNotConfigured(t *testing.T) {
	srv, mock, access := newTestServer(t, nil)
	expectAttachment(mock, "7/abc.jpg")

	status, env := post(t, srv, "/api/v1/tickets/recognize", `{"attachmentId":42}`, access)
	if status != http.StatusInternalServerError || env.Code != codeVisionUnready {
		t.Fatalf("status=%d env=%+v, want 500/50001", status, env)
	}
}

// 上游限流 → 429 信封 42901。
func TestHTTPRecognizeRateLimited(t *testing.T) {
	llm := &fakeLLM{err: mapLLMError(rateLimitErr())}
	srv, mock, access := newTestServer(t, llm)
	expectAttachment(mock, "7/abc.jpg")

	status, env := post(t, srv, "/api/v1/tickets/recognize", `{"attachmentId":42}`, access)
	if status != http.StatusTooManyRequests || env.Code != codeRateLimited {
		t.Fatalf("status=%d env=%+v, want 429/42901", status, env)
	}
}

// 无 token → 401（不落到 service）。
func TestHTTPRecognizeUnauthorized(t *testing.T) {
	srv, _, _ := newTestServer(t, &fakeLLM{out: goodOutput()})
	status, _ := post(t, srv, "/api/v1/tickets/recognize", `{"attachmentId":42}`, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", status)
	}
}
