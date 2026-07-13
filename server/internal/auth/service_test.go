package auth

// service 层单测：sqlmock（QueryMatcherEqual 直接复用 SQL 常量，防实现/断言漂移）。
// 本机无 Docker/MySQL（见 platform/db 测试说明），DB 行为用脚本化期望验证；
// SQL 语句与真实 schema 的对齐由 migrations/0001 契约摘要保证。

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/token"
)

var fixedNow = time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)

func newTestService(t *testing.T) (*service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	svc := &service{
		db:         db,
		tm:         token.NewManager("service-test-secret"),
		accessTTL:  15 * time.Minute,
		refreshTTL: 720 * time.Hour,
		now:        func() time.Time { return fixedNow },
	}
	return svc, mock
}

func wantAppErr(t *testing.T, err error, code int) *apperr.Error {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *apperr.Error", err)
	}
	if ae.Code != code {
		t.Fatalf("code = %d, want %d", ae.Code, code)
	}
	return ae
}

func refreshRows(id, uid int64, expiresAt time.Time, revokedAt any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "expires_at", "revoked_at"}).
		AddRow(id, uid, expiresAt, revokedAt)
}

// TestRegisterIssuesTokens 注册成功：同事务插用户 + refresh，access 可验签出 uid。
func TestRegisterIssuesTokens(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectBegin()
	mock.ExpectExec(sqlInsertUser).
		WithArgs("new@example.com", sqlmock.AnyArg(), "小拾", fixedNow).
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectExec(sqlInsertRefresh).
		WithArgs(int64(42), sqlmock.AnyArg(), fixedNow.Add(720*time.Hour), fixedNow).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	res, err := svc.register(context.Background(), registerData{
		Email: "new@example.com", Password: "sup3rsecret", Nickname: "小拾",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.ID != 42 || res.User.Email != "new@example.com" || res.User.Nickname != "小拾" {
		t.Fatalf("user = %+v", res.User)
	}
	if res.User.CreatedAt != "2026-07-13T12:00:00Z" {
		t.Fatalf("createdAt = %q, want RFC3339 UTC", res.User.CreatedAt)
	}
	uid, err := svc.tm.Verify(res.AccessToken)
	if err != nil || uid != 42 {
		t.Fatalf("access verify = (%d, %v), want (42, nil)", uid, err)
	}
	if len(res.RefreshToken) != refreshTokenBytes*2 {
		t.Fatalf("refresh token len = %d, want %d hex chars", len(res.RefreshToken), refreshTokenBytes*2)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRegisterDuplicateEmail40901 撞 uk_users_email → 40901。
func TestRegisterDuplicateEmail40901(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectBegin()
	mock.ExpectExec(sqlInsertUser).
		WithArgs("dup@example.com", sqlmock.AnyArg(), "小拾", fixedNow).
		WillReturnError(&mysql.MySQLError{Number: mysqlErrDuplicateEntry, Message: "Duplicate entry"})
	mock.ExpectRollback()

	_, err := svc.register(context.Background(), registerData{
		Email: "dup@example.com", Password: "sup3rsecret", Nickname: "小拾",
	})
	wantAppErr(t, err, apperr.CodeEmailTaken)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestLogin40103NoUserEnumeration 邮箱不存在与密码错误必须同 code 同 message。
func TestLogin40103NoUserEnumeration(t *testing.T) {
	svc, mock := newTestService(t)

	// 邮箱不存在。
	mock.ExpectQuery(sqlSelectUserByEmail).
		WithArgs("ghost@example.com").
		WillReturnError(sql.ErrNoRows)
	_, err1 := svc.login(context.Background(), loginData{Email: "ghost@example.com", Password: "whatever123"})
	ae1 := wantAppErr(t, err1, apperr.CodeBadCredentials)

	// 密码错误。
	realHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(sqlSelectUserByEmail).
		WithArgs("real@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "nickname", "created_at"}).
			AddRow(int64(7), "real@example.com", realHash, "小拾", fixedNow))
	_, err2 := svc.login(context.Background(), loginData{Email: "real@example.com", Password: "wrong-password"})
	ae2 := wantAppErr(t, err2, apperr.CodeBadCredentials)

	if ae1.Msg != ae2.Msg {
		t.Fatalf("messages differ (enumeration leak): %q vs %q", ae1.Msg, ae2.Msg)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestLoginSuccess 正确口令：返回用户 + 新 token 对。
func TestLoginSuccess(t *testing.T) {
	svc, mock := newTestService(t)
	realHash, err := hashPassword("correct-password")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	mock.ExpectQuery(sqlSelectUserByEmail).
		WithArgs("real@example.com").
		WillReturnRows(sqlmock.NewRows([]string{"id", "email", "password_hash", "nickname", "created_at"}).
			AddRow(int64(7), "real@example.com", realHash, "小拾", created))
	mock.ExpectExec(sqlInsertRefresh).
		WithArgs(int64(7), sqlmock.AnyArg(), fixedNow.Add(720*time.Hour), fixedNow).
		WillReturnResult(sqlmock.NewResult(2, 1))

	res, err := svc.login(context.Background(), loginData{Email: "real@example.com", Password: "correct-password"})
	if err != nil {
		t.Fatal(err)
	}
	if res.User.ID != 7 || res.User.CreatedAt != "2026-01-02T03:04:05Z" {
		t.Fatalf("user = %+v", res.User)
	}
	if uid, err := svc.tm.Verify(res.AccessToken); err != nil || uid != 7 {
		t.Fatalf("access verify = (%d, %v), want (7, nil)", uid, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRefreshRotationRevokesOld 旋转：同事务内先吊销旧 token 行，再插新行。
func TestRefreshRotationRevokesOld(t *testing.T) {
	svc, mock := newTestService(t)
	const raw = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	mock.ExpectBegin()
	mock.ExpectQuery(sqlSelectRefreshForUpdate).
		WithArgs(hashToken(raw)).
		WillReturnRows(refreshRows(11, 3, fixedNow.Add(time.Hour), nil))
	mock.ExpectExec(sqlRevokeRefreshByID).
		WithArgs(fixedNow, int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(sqlInsertRefresh).
		WithArgs(int64(3), sqlmock.AnyArg(), fixedNow.Add(720*time.Hour), fixedNow).
		WillReturnResult(sqlmock.NewResult(12, 1))
	mock.ExpectCommit()

	pair, err := svc.refresh(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if pair.RefreshToken == raw {
		t.Fatal("refresh must rotate to a new token")
	}
	if uid, err := svc.tm.Verify(pair.AccessToken); err != nil || uid != 3 {
		t.Fatalf("access verify = (%d, %v), want (3, nil)", uid, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestRefresh40102 不存在 / 已过期，一律 40102 且不发新 token（已吊销见下方家族吊销测试）。
func TestRefresh40102(t *testing.T) {
	cases := []struct {
		name string
		prep func(mock sqlmock.Sqlmock)
	}{
		{"unknown token", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(sqlSelectRefreshForUpdate).
				WithArgs(sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
		}},
		{"expired token", func(mock sqlmock.Sqlmock) {
			mock.ExpectQuery(sqlSelectRefreshForUpdate).
				WithArgs(sqlmock.AnyArg()).
				WillReturnRows(refreshRows(11, 3, fixedNow.Add(-time.Second), nil))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, mock := newTestService(t)
			mock.ExpectBegin()
			tc.prep(mock)
			mock.ExpectRollback()

			_, err := svc.refresh(context.Background(), "whatever-raw-token")
			wantAppErr(t, err, apperr.CodeRefreshInvalid)
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestRefreshReuseRevokesFamily 重放检测：已吊销 token 再次使用 → 吊销该用户
// 全部未吊销 refresh（家族吊销）并 commit 落库，对外仍统一 40102，不发新 token。
func TestRefreshReuseRevokesFamily(t *testing.T) {
	svc, mock := newTestService(t)
	const raw = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	mock.ExpectBegin()
	mock.ExpectQuery(sqlSelectRefreshForUpdate).
		WithArgs(hashToken(raw)).
		WillReturnRows(refreshRows(11, 3, fixedNow.Add(time.Hour), fixedNow.Add(-time.Minute)))
	mock.ExpectExec(sqlRevokeAllRefreshByUser).
		WithArgs(fixedNow, int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	_, err := svc.refresh(context.Background(), raw)
	wantAppErr(t, err, apperr.CodeRefreshInvalid)
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// TestLogoutIdempotent 未匹配任何行（未知/已吊销）也返回成功。
func TestLogoutIdempotent(t *testing.T) {
	svc, mock := newTestService(t)
	mock.ExpectExec(sqlRevokeRefreshByHash).
		WithArgs(fixedNow, hashToken("some-raw-token")).
		WillReturnResult(sqlmock.NewResult(0, 0))

	if err := svc.logout(context.Background(), "some-raw-token"); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
