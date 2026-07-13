package auth

// handler 层测试：参数校验 40001、错误码 → HTTP 状态映射、登录限流。

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/token"
)

func newHandlerServer(t *testing.T) (*httptest.Server, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	srv := httptest.NewServer(Routes(db, token.NewManager("handler-test-secret"), 15*time.Minute, 720*time.Hour))
	t.Cleanup(srv.Close)
	return srv, mock
}

// TestValidation40001 各端点缺参/非法参数 → 40001 + HTTP 400。
func TestValidation40001(t *testing.T) {
	srv, _ := newHandlerServer(t)
	cases := []struct {
		name, path string
		payload    map[string]string
	}{
		{"register missing email", "/register", map[string]string{"password": "sup3rsecret", "nickname": "n"}},
		{"register bad email", "/register", map[string]string{"email": "not-an-email", "password": "sup3rsecret", "nickname": "n"}},
		{"register short password", "/register", map[string]string{"email": "a@b.com", "password": "short", "nickname": "n"}},
		{"register missing nickname", "/register", map[string]string{"email": "a@b.com", "password": "sup3rsecret"}},
		{"register blank nickname", "/register", map[string]string{"email": "a@b.com", "password": "sup3rsecret", "nickname": "   "}},
		{"login missing email", "/login", map[string]string{"password": "whatever123"}},
		{"login missing password", "/login", map[string]string{"email": "a@b.com"}},
		{"refresh missing token", "/refresh", map[string]string{}},
		{"logout missing token", "/logout", map[string]string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, env := postJSON(t, srv.URL+tc.path, tc.payload)
			if status != http.StatusBadRequest || env.Code != 40001 {
				t.Fatalf("status/code = %d/%d, want 400/40001", status, env.Code)
			}
		})
	}
}

// TestInvalidJSONBody40001 非法 JSON → 40001。
func TestInvalidJSONBody40001(t *testing.T) {
	srv, _ := newHandlerServer(t)
	resp, err := http.Post(srv.URL+"/register", "application/json", strings.NewReader("{not json"))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// TestRegisterDuplicateHTTP409 40901 → HTTP 409。
func TestRegisterDuplicateHTTP409(t *testing.T) {
	srv, mock := newHandlerServer(t)
	mock.ExpectBegin()
	mock.ExpectExec(sqlInsertUser).
		WithArgs("dup@example.com", sqlmock.AnyArg(), "n", sqlmock.AnyArg()).
		WillReturnError(&mysql.MySQLError{Number: mysqlErrDuplicateEntry, Message: "Duplicate entry"})
	mock.ExpectRollback()

	status, env := postJSON(t, srv.URL+"/register",
		map[string]string{"email": "dup@example.com", "password": "sup3rsecret", "nickname": "n"})
	if status != http.StatusConflict || env.Code != 40901 {
		t.Fatalf("status/code = %d/%d, want 409/40901", status, env.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestLoginBadCredentialsHTTP401 40103 → HTTP 401，且邮箱大小写归一后查询。
func TestLoginBadCredentialsHTTP401(t *testing.T) {
	srv, mock := newHandlerServer(t)
	mock.ExpectQuery(sqlSelectUserByEmail).
		WithArgs("upper@example.com"). // handler 层已 trim + 小写
		WillReturnError(sql.ErrNoRows)

	status, env := postJSON(t, srv.URL+"/login",
		map[string]string{"email": "  UPPER@Example.COM ", "password": "whatever123"})
	if status != http.StatusUnauthorized || env.Code != 40103 {
		t.Fatalf("status/code = %d/%d, want 401/40103", status, env.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestLoginRateLimited 同 IP 第 6 次登录（1 分钟窗口内）被拒；校验失败的请求同样计数。
func TestLoginRateLimited(t *testing.T) {
	srv, mock := newHandlerServer(t)
	for range 5 {
		mock.ExpectQuery(sqlSelectUserByEmail).
			WithArgs("rl@example.com").
			WillReturnError(sql.ErrNoRows)
	}
	for i := range 5 {
		status, env := postJSON(t, srv.URL+"/login",
			map[string]string{"email": "rl@example.com", "password": "whatever123"})
		if status != http.StatusUnauthorized || env.Code != 40103 {
			t.Fatalf("attempt %d: status/code = %d/%d, want 401/40103", i+1, status, env.Code)
		}
	}
	status, env := postJSON(t, srv.URL+"/login",
		map[string]string{"email": "rl@example.com", "password": "whatever123"})
	if status != http.StatusBadRequest || env.Code != 40001 {
		t.Fatalf("6th attempt: status/code = %d/%d, want 400/40001 (rate limited)", status, env.Code)
	}
	if !strings.Contains(env.Message, "too many") {
		t.Fatalf("6th attempt message = %q, want rate-limit hint", env.Message)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRateLimitOnlyLogin register / refresh / logout 不受登录限流影响。
func TestRateLimitOnlyLogin(t *testing.T) {
	srv, mock := newHandlerServer(t)
	for range 5 {
		mock.ExpectQuery(sqlSelectUserByEmail).
			WithArgs("rl2@example.com").
			WillReturnError(sql.ErrNoRows)
	}
	for range 5 {
		postJSON(t, srv.URL+"/login", map[string]string{"email": "rl2@example.com", "password": "whatever123"})
	}
	// 限流已触发，但 refresh 走自己的路径（40102 而非 40001 限流拒绝）。
	mock.ExpectBegin()
	mock.ExpectQuery(sqlSelectRefreshForUpdate).
		WithArgs(sqlmock.AnyArg()).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()
	status, env := postJSON(t, srv.URL+"/refresh", map[string]string{"refreshToken": "deadbeef"})
	if status != http.StatusUnauthorized || env.Code != 40102 {
		t.Fatalf("refresh under login-limit: status/code = %d/%d, want 401/40102", status, env.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
