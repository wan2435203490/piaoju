package auth

// 全链路集成测试：HTTP 层走真实 chi 路由 + httpx 信封 + middleware.Auth，
// DB 层用 sqlmock 按步脚本化（register → login → refresh(旧 token 吊销) → logout）。
// 路由挂载方式与 cmd/api/router.go 的挂载点注释一致。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/httpx"
	"piaoju/internal/platform/token"
)

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type authData struct {
	User         User   `json:"user"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

func postJSON(t *testing.T, url string, payload any) (int, envelope) {
	t.Helper()
	b, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, env
}

func getWithBearer(t *testing.T, url, access string) (int, envelope) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer "+access)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, env
}

var hexTokenRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestAuthFullChain(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	// 按 router.go 挂载点接线：/auth 公开、/ping 挂 Auth（同一个 token.Manager）。
	tm := token.NewManager("chain-test-secret")
	r := chi.NewRouter()
	r.Route("/api/v1", func(api chi.Router) {
		api.Mount("/auth", Routes(db, tm, 15*time.Minute, 720*time.Hour))
		api.Group(func(sec chi.Router) {
			sec.Use(middleware.Auth(tm))
			sec.Get("/ping", func(w http.ResponseWriter, req *http.Request) {
				httpx.OK(w, map[string]any{"uid": middleware.UID(req.Context())})
			})
		})
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	const (
		email    = "chain@example.com"
		password = "sup3rsecret"
		nickname = "链条侠"
	)

	// ── 1. register ──────────────────────────────────────────────────────────
	mock.ExpectBegin()
	mock.ExpectExec(sqlInsertUser).
		WithArgs(email, sqlmock.AnyArg(), nickname, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectExec(sqlInsertRefresh).
		WithArgs(int64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	status, env := postJSON(t, srv.URL+"/api/v1/auth/register",
		map[string]string{"email": email, "password": password, "nickname": nickname})
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("register status/code = %d/%d, body = %s", status, env.Code, env.Data)
	}
	var reg authData
	if err := json.Unmarshal(env.Data, &reg); err != nil {
		t.Fatal(err)
	}
	if reg.User.ID != 1 || reg.User.Email != email || reg.User.Nickname != nickname {
		t.Fatalf("register user = %+v", reg.User)
	}
	if !hexTokenRe.MatchString(reg.RefreshToken) {
		t.Fatalf("refresh token %q not 64-char hex", reg.RefreshToken)
	}
	if uid, err := tm.Verify(reg.AccessToken); err != nil || uid != 1 {
		t.Fatalf("register access verify = (%d, %v), want (1, nil)", uid, err)
	}

	// access token 直接可用于受保护路由（对齐 S1 中间件）。
	if status, env := getWithBearer(t, srv.URL+"/api/v1/ping", reg.AccessToken); status != http.StatusOK || env.Code != 0 {
		t.Fatalf("ping with fresh access = %d/%d", status, env.Code)
	}

	// 过期 access → 40101（错误码测试 40101）。
	expired, err := tm.Sign(1, -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if status, env := getWithBearer(t, srv.URL+"/api/v1/ping", expired); status != http.StatusUnauthorized || env.Code != 40101 {
		t.Fatalf("ping with expired access = %d/%d, want 401/40101", status, env.Code)
	}

	// ── 2. login ─────────────────────────────────────────────────────────────
	pwHash, err := hashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	mock.ExpectQuery(sqlSelectUserByEmail).
		WithArgs(email).
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "nickname", "created_at"}).
			AddRow(int64(1), email, pwHash, nickname, created))
	mock.ExpectExec(sqlInsertRefresh).
		WithArgs(int64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(2, 1))

	status, env = postJSON(t, srv.URL+"/api/v1/auth/login",
		map[string]string{"email": email, "password": password})
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("login status/code = %d/%d", status, env.Code)
	}
	var lg authData
	if err := json.Unmarshal(env.Data, &lg); err != nil {
		t.Fatal(err)
	}
	if lg.User.CreatedAt != "2026-07-01T00:00:00Z" {
		t.Fatalf("login createdAt = %q", lg.User.CreatedAt)
	}

	// ── 3. refresh（用 register 发的 R1；旧 token 同事务吊销）────────────────
	r1 := reg.RefreshToken
	mock.ExpectBegin()
	mock.ExpectQuery(sqlSelectRefreshForUpdate).
		WithArgs(hashToken(r1)). // 必须按「真实签发 token 的 sha256」查库
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "expires_at", "revoked_at"}).
			AddRow(int64(1), int64(1), time.Now().UTC().Add(720*time.Hour), nil))
	mock.ExpectExec(sqlRevokeRefreshByID).
		WithArgs(sqlmock.AnyArg(), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(sqlInsertRefresh).
		WithArgs(int64(1), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(3, 1))
	mock.ExpectCommit()

	status, env = postJSON(t, srv.URL+"/api/v1/auth/refresh", map[string]string{"refreshToken": r1})
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("refresh status/code = %d/%d", status, env.Code)
	}
	var pair tokenPair
	if err := json.Unmarshal(env.Data, &pair); err != nil {
		t.Fatal(err)
	}
	if pair.RefreshToken == r1 || !hexTokenRe.MatchString(pair.RefreshToken) {
		t.Fatalf("refresh must rotate: new = %q", pair.RefreshToken)
	}
	if uid, err := tm.Verify(pair.AccessToken); err != nil || uid != 1 {
		t.Fatalf("refreshed access verify = (%d, %v), want (1, nil)", uid, err)
	}

	// ── 4. 旧 R1 已死：再次 refresh → 40102 ──────────────────────────────────
	mock.ExpectBegin()
	mock.ExpectQuery(sqlSelectRefreshForUpdate).
		WithArgs(hashToken(r1)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "expires_at", "revoked_at"}).
			AddRow(int64(1), int64(1), time.Now().UTC().Add(720*time.Hour), time.Now().UTC()))
	mock.ExpectRollback()

	status, env = postJSON(t, srv.URL+"/api/v1/auth/refresh", map[string]string{"refreshToken": r1})
	if status != http.StatusUnauthorized || env.Code != 40102 {
		t.Fatalf("reused refresh = %d/%d, want 401/40102", status, env.Code)
	}

	// ── 5. logout（吊销 R2）→ data 恒为 null ─────────────────────────────────
	mock.ExpectExec(sqlRevokeRefreshByHash).
		WithArgs(sqlmock.AnyArg(), hashToken(pair.RefreshToken)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	status, env = postJSON(t, srv.URL+"/api/v1/auth/logout", map[string]string{"refreshToken": pair.RefreshToken})
	if status != http.StatusOK || env.Code != 0 {
		t.Fatalf("logout status/code = %d/%d", status, env.Code)
	}
	if string(env.Data) != "null" {
		t.Fatalf("logout data = %s, want null", env.Data)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
