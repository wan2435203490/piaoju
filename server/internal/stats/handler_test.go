package stats

// handler 层测试：真 HTTP（chi + middleware.Auth + JWT）+ sqlmock，
// 端到端校验参数解析、UTC 边界与响应信封（对齐 cmd/api/router_test.go 风格）。

import (
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

	tm := token.NewManager("stats-test-secret")
	access, err := tm.Sign(httpUID, time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))
			sec.Mount("/stats", Routes(db)) // 与主线程挂载方式一致
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

func get(t *testing.T, srv *httptest.Server, path, access string) (int, envelope, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if access != "" {
		req.Header.Set("Authorization", "Bearer "+access)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("bad envelope %q: %v", raw, err)
	}
	return resp.StatusCode, env, strings.TrimSpace(string(raw))
}

// TestHTTPRequiresAuth 两个端点未带 token 均 40101 + HTTP 401。
func TestHTTPRequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	for _, p := range []string{"/api/v1/stats/monthly?month=2026-07", "/api/v1/stats/tickets?year=2026"} {
		status, env, _ := get(t, srv, p, "")
		if status != http.StatusUnauthorized || env.Code != 40101 {
			t.Fatalf("GET %s → status=%d code=%d, want 401/40101", p, status, env.Code)
		}
	}
}

// TestHTTPParamValidation month/year 必填 + 格式校验，不碰 DB。
func TestHTTPParamValidation(t *testing.T) {
	srv, mock, access := newTestServer(t)
	cases := []string{
		"/api/v1/stats/monthly",               // month 缺失
		"/api/v1/stats/monthly?month=2026-13", // 月份越界
		"/api/v1/stats/monthly?month=2026-7",  // 缺前导零
		"/api/v1/stats/monthly?month=garbage", // 非日期
		"/api/v1/stats/tickets",               // year 缺失
		"/api/v1/stats/tickets?year=abc",      // 非数字
		"/api/v1/stats/tickets?year=0",        // 越界
		"/api/v1/stats/tickets?year=10000",    // 越界
	}
	for _, p := range cases {
		status, env, _ := get(t, srv, p, access)
		if status != http.StatusBadRequest || env.Code != 40001 {
			t.Fatalf("GET %s → status=%d code=%d, want 400/40001", p, status, env.Code)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("validation should not hit DB: %v", err)
	}
}

// TestHTTPMonthlyExactEnvelope 月度统计全链路：JWT uid 进 SQL、UTC 月边界、
// 信封逐字节对契约 §7（字段名/嵌套结构）。数字沿用 service_test 的手算 fixture。
func TestHTTPMonthlyExactEnvelope(t *testing.T) {
	srv, mock, access := newTestServer(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyTotals)).
		WithArgs(httpUID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "cents"}).
			AddRow("expense", 7500).AddRow("income", 500000))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByCategory)).
		WithArgs(httpUID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"category_id", "cents", "count"}).
			AddRow(2, 3800, 1).AddRow(1, 3700, 2))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByDay)).
		WithArgs(httpUID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"d", "cents"}).
			AddRow(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 5000).
			AddRow(time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), 2500))

	_, _, raw := get(t, srv, "/api/v1/stats/monthly?month=2026-07", access)
	want := `{"code":0,"message":"ok","data":{"expenseCents":7500,"incomeCents":500000,` +
		`"byCategory":[{"categoryId":2,"cents":3800,"count":1},{"categoryId":1,"cents":3700,"count":2}],` +
		`"byDay":[{"date":"2026-07-01","expenseCents":5000},{"date":"2026-07-03","expenseCents":2500}]}}`
	if raw != want {
		t.Fatalf("body = %s\nwant  %s", raw, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPMonthlyDecemberBoundary month=2026-12 → UTC 半开区间 [2026-12-01, 2027-01-01)。
func TestHTTPMonthlyDecemberBoundary(t *testing.T) {
	srv, mock, access := newTestServer(t)
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyTotals)).
		WithArgs(httpUID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "cents"}))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByCategory)).
		WithArgs(httpUID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"category_id", "cents", "count"}))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByDay)).
		WithArgs(httpUID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"d", "cents"}))

	_, env, _ := get(t, srv, "/api/v1/stats/monthly?month=2026-12", access)
	if env.Code != 0 {
		t.Fatalf("code = %d, want 0", env.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPTicketsExactEnvelope 年度票夹统计：UTC 年边界 + 信封逐字节对契约 §7。
// 数字对齐 web fixtures stats-tickets.json（total=5，五 kind 各一张）。
func TestHTTPTicketsExactEnvelope(t *testing.T) {
	srv, mock, access := newTestServer(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(sqlTicketsByKind)).
		WithArgs(httpUID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"kind", "count", "cents"}).
			AddRow("movie", 1, 9900).
			AddRow("show", 1, 28000).
			AddRow("attraction", 1, 4500).
			AddRow("train", 1, 62300).
			AddRow("flight", 1, 128000))

	_, _, raw := get(t, srv, "/api/v1/stats/tickets?year=2026", access)
	want := `{"code":0,"message":"ok","data":{"total":5,"byKind":[` +
		`{"kind":"movie","count":1,"cents":9900},` +
		`{"kind":"show","count":1,"cents":28000},` +
		`{"kind":"attraction","count":1,"cents":4500},` +
		`{"kind":"train","count":1,"cents":62300},` +
		`{"kind":"flight","count":1,"cents":128000}]}}`
	if raw != want {
		t.Fatalf("body = %s\nwant  %s", raw, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPTicketsEmptyExactJSON 空年信封：byKind=[] 非 null。
func TestHTTPTicketsEmptyExactJSON(t *testing.T) {
	srv, mock, access := newTestServer(t)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(sqlTicketsByKind)).
		WithArgs(httpUID, start, end).
		WillReturnRows(sqlmock.NewRows([]string{"kind", "count", "cents"}))

	_, _, raw := get(t, srv, "/api/v1/stats/tickets?year=2025", access)
	want := `{"code":0,"message":"ok","data":{"total":0,"byKind":[]}}`
	if raw != want {
		t.Fatalf("body = %s, want %s", raw, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
