package transaction

// handler 层测试：真 HTTP（chi + middleware.Auth + JWT）+ sqlmock，
// 端到端校验参数解析、错误码与响应信封（对齐 cmd/api/router_test.go 风格）。

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

	tm := token.NewManager("transaction-test-secret")
	access, err := tm.Sign(httpUID, time.Minute)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}

	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))
			sec.Mount("/transactions", Routes(db)) // 与主线程挂载方式一致
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

func do(t *testing.T, srv *httptest.Server, method, path, body, access string) (int, envelope, string) {
	t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
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

// TestHTTPRequiresAuth 未带 token → 40101 + HTTP 401。
func TestHTTPRequiresAuth(t *testing.T) {
	srv, _, _ := newTestServer(t)
	status, env, _ := do(t, srv, http.MethodGet, "/api/v1/transactions", "", "")
	if status != http.StatusUnauthorized || env.Code != 40101 {
		t.Fatalf("status=%d code=%d, want 401/40101", status, env.Code)
	}
}

// TestHTTPListParamValidation 非法查询参数逐项拒绝，不碰 DB。
func TestHTTPListParamValidation(t *testing.T) {
	srv, mock, access := newTestServer(t)
	cases := []struct {
		query    string
		wantCode int
	}{
		{"?month=2026-13", 40001},    // 月份越界
		{"?month=2026-7", 40001},     // 缺前导零
		{"?month=202607", 40001},     // 缺分隔符
		{"?categoryId=0", 40001},     // 非正 id
		{"?categoryId=abc", 40001},   // 非数字
		{"?direction=refund", 40002}, // 非法枚举
		{"?limit=0", 40001},          // 下界
		{"?limit=101", 40001},        // 上界（max 100）
		{"?limit=abc", 40001},        // 非数字
		{"?cursor=!!!bad!!!", 40001}, // 非法游标
	}
	for _, c := range cases {
		status, env, _ := do(t, srv, http.MethodGet, "/api/v1/transactions"+c.query, "", access)
		if env.Code != c.wantCode {
			t.Fatalf("GET %s → code %d, want %d", c.query, env.Code, c.wantCode)
		}
		if status != http.StatusBadRequest {
			t.Fatalf("GET %s → status %d, want 400", c.query, status)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("validation should not hit DB: %v", err)
	}
}

// TestHTTPListDecemberBoundaryExactJSON month=2026-12 跨年边界 [12-01, 次年 01-01)（UTC），
// 空结果信封逐字节对契约：items=[] 非 null、nextCursor=null。
func TestHTTPListDecemberBoundaryExactJSON(t *testing.T) {
	srv, mock, access := newTestServer(t)
	start := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	q := sqlSelectTx + " WHERE tx.user_id = ? AND tx.deleted_at IS NULL" +
		" AND tx.occurred_at >= ? AND tx.occurred_at < ?" +
		" ORDER BY tx.occurred_at DESC, tx.id DESC LIMIT ?"
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(httpUID, start, end, 51). // 默认 limit=50（契约 §4）+1 探页
		WillReturnRows(txRowsCols())

	_, _, raw := do(t, srv, http.MethodGet, "/api/v1/transactions?month=2026-12", "", access)
	want := `{"code":0,"message":"ok","data":{"items":[],"nextCursor":null}}`
	if raw != want {
		t.Fatalf("body = %s, want %s", raw, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPCreateHappyPath POST 全链路：JWT uid 进 SQL、时间字段转 UTC、响应完整实体。
func TestHTTPCreateHappyPath(t *testing.T) {
	srv, mock, access := newTestServer(t)
	// +08:00 输入必须归一化为 UTC 03:04:05 入库。
	occurredUTC := time.Date(2026, 7, 10, 3, 4, 5, 0, time.UTC)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, httpUID).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTx)).
		WithArgs(txID, httpUID, int64(2600), "expense", int64(1), "晚饭", occurredUTC,
			"alipay", sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	body := `{"id":"` + txID + `","amountCents":2600,"direction":"expense","categoryId":1,` +
		`"note":"晚饭","occurredAt":"2026-07-10T11:04:05+08:00","paymentMethod":"alipay"}`
	status, env, _ := do(t, srv, http.MethodPost, "/api/v1/transactions", body, access)
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("status=%d code=%d msg=%s, want 200/0", status, env.Code, env.Message)
	}
	var tr Transaction
	if err := json.Unmarshal(env.Data, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.ID != txID || tr.OccurredAt != "2026-07-10T03:04:05Z" || tr.TicketID != nil {
		t.Fatalf("data = %+v", tr)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPCreateStaleConflict409 updatedAt 更旧的重放 → 40902 + HTTP 409。
func TestHTTPCreateStaleConflict409(t *testing.T) {
	srv, mock, access := newTestServer(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, httpUID).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "created_at", "updated_at", "tk_id"}).
			AddRow("expense", created, fixedNow, nil)) // 服务端更新于 2026-07-13
	mock.ExpectRollback()

	body := `{"id":"` + txID + `","amountCents":2600,"direction":"expense","categoryId":1,` +
		`"occurredAt":"2026-07-10T03:04:05Z","updatedAt":"2026-07-01T00:00:00Z"}`
	status, env, _ := do(t, srv, http.MethodPost, "/api/v1/transactions", body, access)
	if status != http.StatusConflict || env.Code != 40902 {
		t.Fatalf("status=%d code=%d, want 409/40902", status, env.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPCreateValidation 必填/枚举/格式校验，不碰 DB。
func TestHTTPCreateValidation(t *testing.T) {
	srv, mock, access := newTestServer(t)
	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"missing id", `{"amountCents":1,"direction":"expense","categoryId":1,"occurredAt":"2026-07-10T03:04:05Z"}`, 40001},
		{"bad uuid", `{"id":"not-a-uuid","amountCents":1,"direction":"expense","categoryId":1,"occurredAt":"2026-07-10T03:04:05Z"}`, 40001},
		{"negative amount", `{"id":"` + txID + `","amountCents":-1,"direction":"expense","categoryId":1,"occurredAt":"2026-07-10T03:04:05Z"}`, 40001},
		{"missing direction", `{"id":"` + txID + `","amountCents":1,"categoryId":1,"occurredAt":"2026-07-10T03:04:05Z"}`, 40001},
		{"bad direction enum", `{"id":"` + txID + `","amountCents":1,"direction":"refund","categoryId":1,"occurredAt":"2026-07-10T03:04:05Z"}`, 40002},
		{"bad payment enum", `{"id":"` + txID + `","amountCents":1,"direction":"expense","categoryId":1,"occurredAt":"2026-07-10T03:04:05Z","paymentMethod":"bitcoin"}`, 40002},
		{"missing occurredAt", `{"id":"` + txID + `","amountCents":1,"direction":"expense","categoryId":1}`, 40001},
		{"bad occurredAt", `{"id":"` + txID + `","amountCents":1,"direction":"expense","categoryId":1,"occurredAt":"2026/07/10"}`, 40001},
		{"bad json", `{`, 40001},
	}
	for _, c := range cases {
		_, env, _ := do(t, srv, http.MethodPost, "/api/v1/transactions", c.body, access)
		if env.Code != c.wantCode {
			t.Fatalf("%s → code %d, want %d", c.name, env.Code, c.wantCode)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("validation should not hit DB: %v", err)
	}
}

// TestHTTPPatchBodyIDMismatch body.id 与路径 id 不一致 → 40001，不碰 DB。
func TestHTTPPatchBodyIDMismatch(t *testing.T) {
	srv, mock, access := newTestServer(t)
	_, env, _ := do(t, srv, http.MethodPatch, "/api/v1/transactions/"+txID,
		`{"id":"e1b041cd-b29c-4f45-a48c-9294b6a9bf8f","amountCents":1}`, access)
	if env.Code != 40001 {
		t.Fatalf("code = %d, want 40001", env.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("validation should not hit DB: %v", err)
	}
}

// TestHTTPDeleteTicketLinkedExactMessage 票据关联交易删除被拒：40001 + 契约原文提示。
func TestHTTPDeleteTicketLinkedExactMessage(t *testing.T) {
	srv, mock, access := newTestServer(t)
	r := sampleRow(txID, occurred)
	r.TicketID.Valid, r.TicketID.String = true, "2afe70af-2033-4e5d-b8d4-43edad37fdcb"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, httpUID).
		WillReturnRows(addRow(txRowsCols(), r))
	mock.ExpectRollback()

	status, env, _ := do(t, srv, http.MethodDelete, "/api/v1/transactions/"+txID, "", access)
	if status != http.StatusBadRequest || env.Code != 40001 || env.Message != "请从票夹删除该票据" {
		t.Fatalf("status=%d code=%d msg=%q, want 400/40001/请从票夹删除该票据", status, env.Code, env.Message)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPDeleteOKExactJSON 删除成功 → data: null（契约 v1.1），信封逐字节对。
func TestHTTPDeleteOKExactJSON(t *testing.T) {
	srv, mock, access := newTestServer(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, httpUID).
		WillReturnRows(addRow(txRowsCols(), sampleRow(txID, occurred)))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTx)).
		WithArgs(sqlmock.AnyArg(), sqlmock.AnyArg(), txID, httpUID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	_, _, raw := do(t, srv, http.MethodDelete, "/api/v1/transactions/"+txID, "", access)
	want := `{"code":0,"message":"ok","data":null}`
	if raw != want {
		t.Fatalf("body = %s, want %s", raw, want)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestHTTPUserIsolation 用户 B 的 token 打用户 A 的行：uid 走进 SQL 条件 → 无行 → 40401。
func TestHTTPUserIsolation(t *testing.T) {
	srv, mock, _ := newTestServer(t)
	const uidB int64 = 9
	tm := token.NewManager("transaction-test-secret")
	accessB, err := tm.Sign(uidB, time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, uidB). // 断言 B 的 uid 被带进查询（隔离链路成立）
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	status, env, _ := do(t, srv, http.MethodPatch, "/api/v1/transactions/"+txID, `{"amountCents":1}`, accessB)
	if status != http.StatusNotFound || env.Code != 40401 {
		t.Fatalf("status=%d code=%d, want 404/40401（不泄漏他人资源存在性）", status, env.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestParseMonthBoundaries 月参数解析：UTC 半开区间与跨年进位。
func TestParseMonthBoundaries(t *testing.T) {
	start, end, err := parseMonth("2026-12")
	if err != nil {
		t.Fatalf("parseMonth: %v", err)
	}
	if !start.Equal(time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)) ||
		!end.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("boundaries = [%v, %v)", start, end)
	}
	for _, bad := range []string{"2026-13", "2026-0", "2026-7", "26-07", "2026-07-01", "garbage"} {
		if _, _, err := parseMonth(bad); err == nil {
			t.Fatalf("parseMonth(%q) should fail", bad)
		}
	}
}
