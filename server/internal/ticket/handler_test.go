package ticket

// handler 层测试：真 HTTP（chi + middleware.Auth + JWT）+ sqlmock，
// 校验参数解析、错误码与响应信封（对齐 transaction/handler_test.go 风格）。

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/token"
)

const httpUID int64 = 7

func newTestServer(t *testing.T) (*httptest.Server, sqlmock.Sqlmock, string) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	tm := token.NewManager("ticket-test-secret")
	access, err := tm.Sign(httpUID, time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))
			sec.Mount("/tickets", Routes(db, "upload-test-secret")) // 与主线程挂载方式一致
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv, mock, access
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func do(t *testing.T, srv *httptest.Server, method, path, body, access string) (int, envelope) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return resp.StatusCode, env
}

func TestHandlerUnauthorized(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, env := do(t, srv, http.MethodGet, "/api/v1/tickets/", "", "")
	if status != http.StatusUnauthorized || env.Code != 40101 {
		t.Fatalf("status/code = %d/%d, want 401/40101", status, env.Code)
	}
}

func TestHandlerListParamValidation(t *testing.T) {
	srv, _, access := newTestServer(t)
	cases := []struct {
		name, query string
		code        int
	}{
		{"bad kind", "?kind=concert", 40002},
		{"bad year", "?year=abc", 40001},
		{"year out of range", "?year=0", 40001},
		{"bad limit", "?limit=0", 40001},
		{"limit over max", "?limit=101", 40001},
		{"bad cursor", "?cursor=!!!", 40001},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, env := do(t, srv, http.MethodGet, "/api/v1/tickets/"+tc.query, "", access)
			if env.Code != tc.code {
				t.Fatalf("code = %d, want %d", env.Code, tc.code)
			}
		})
	}
}

func TestHandlerCreateInvalidJSON(t *testing.T) {
	srv, _, access := newTestServer(t)
	_, env := do(t, srv, http.MethodPost, "/api/v1/tickets/", "{not json", access)
	if env.Code != 40001 {
		t.Fatalf("code = %d, want 40001", env.Code)
	}
}

func TestHandlerCreateMissingRequired(t *testing.T) {
	srv, _, access := newTestServer(t)
	_, env := do(t, srv, http.MethodPost, "/api/v1/tickets/", `{"kind":"movie"}`, access)
	if env.Code != 40001 {
		t.Fatalf("code = %d, want 40001 (id required)", env.Code)
	}
}

func TestHandlerPatchBodyIDMismatch(t *testing.T) {
	srv, _, access := newTestServer(t)
	_, env := do(t, srv, http.MethodPatch, "/api/v1/tickets/"+tkID, `{"id":"`+otherT+`"}`, access)
	if env.Code != 40001 {
		t.Fatalf("code = %d, want 40001", env.Code)
	}
}

// DELETE 全链路：路径 id 大写归一化 → 软删票 + 交易 → data: null。
func TestHandlerDeleteFlow(t *testing.T) {
	srv, mock, access := newTestServer(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT transaction_id FROM tickets WHERE id = ? AND user_id = ? AND deleted_at IS NULL FOR UPDATE")).
		WithArgs(tkID, httpUID).
		WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}).AddRow(txIDA))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTicket)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), tkID, httpUID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTransaction)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), txIDA, httpUID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	status, env := do(t, srv, http.MethodDelete, "/api/v1/tickets/"+strings.ToUpper(tkID), "", access)
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("status/code = %d/%d, want 200/0", status, env.Code)
	}
	if string(env.Data) != "null" {
		t.Fatalf("data = %s, want null", env.Data)
	}
	mustMeet(t, mock)
}

// 路径 id 非法（连 UUID 都不是）→ 服务层查无此行 → 40401。
func TestHandlerGetNotFound(t *testing.T) {
	srv, mock, access := newTestServer(t)
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTicket)).
		WithArgs(tkID, httpUID).WillReturnError(sql.ErrNoRows)

	status, env := do(t, srv, http.MethodGet, "/api/v1/tickets/"+tkID, "", access)
	if status != http.StatusNotFound || env.Code != 40401 {
		t.Fatalf("status/code = %d/%d, want 404/40401", status, env.Code)
	}
	mustMeet(t, mock)
}
