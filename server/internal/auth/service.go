package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/token"
)

// SQL 常量（测试用 QueryMatcherEqual 复用，防实现/断言漂移）。
//
// user_id 隔离说明（conventions §2）：users / refresh_tokens 是认证入口表，
// 请求尚无已认证用户——email / token_hash 本身即凭据，等价于主键查询；
// 除此两表外本模块不访问任何业务表。
const (
	sqlInsertUser        = "INSERT INTO users (email, password_hash, nickname, created_at) VALUES (?, ?, ?, ?)"
	sqlSelectUserByEmail = "SELECT id, email, password_hash, nickname, created_at FROM users WHERE email = ?"

	sqlInsertRefresh = "INSERT INTO refresh_tokens (user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?)"
	// 刷新旋转在事务内行锁旧 token，防并发重放同一 refresh 换出多对新 token。
	sqlSelectRefreshForUpdate = "SELECT id, user_id, expires_at, revoked_at FROM refresh_tokens WHERE token_hash = ? FOR UPDATE"
	sqlRevokeRefreshByID      = "UPDATE refresh_tokens SET revoked_at = ? WHERE id = ?"
	sqlRevokeRefreshByHash    = "UPDATE refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL"

	mysqlErrDuplicateEntry = 1062
)

// refreshTokenBytes 256-bit 随机 refresh token（hex 后 64 字符）。
const refreshTokenBytes = 32

type service struct {
	db         *sql.DB
	tm         *token.Manager // access JWT 签发（与 middleware.Auth 共用密钥）
	accessTTL  time.Duration  // config.AccessTTL（默认 15m）
	refreshTTL time.Duration  // config.RefreshTTL（默认 720h）
	now        func() time.Time
}

// errBadCredentials 40103：邮箱不存在与密码错误共用同一 code + message，防账号枚举。
func errBadCredentials() error {
	return apperr.New(apperr.CodeBadCredentials, "incorrect email or password")
}

// errRefreshInvalid 40102：不存在 / 已吊销 / 已过期共用同一 message，不泄漏具体原因。
func errRefreshInvalid() error {
	return apperr.New(apperr.CodeRefreshInvalid, "invalid refresh token")
}

// register 建号 + 签发首对 token（同事务：用户与 refresh 要么都有要么都无）。
// 邮箱撞唯一键 → 40901（并发注册也由唯一键兜底，无 TOCTOU）。
func (s *service) register(ctx context.Context, d registerData) (*authResult, error) {
	hash, err := hashPassword(d.Password)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC().Truncate(time.Millisecond)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // commit 后为 ErrTxDone no-op

	res, err := tx.ExecContext(ctx, sqlInsertUser, d.Email, hash, d.Nickname, now)
	if err != nil {
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == mysqlErrDuplicateEntry {
			return nil, apperr.New(apperr.CodeEmailTaken, "email already registered")
		}
		return nil, fmt.Errorf("auth: insert user: %w", err)
	}
	uid, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("auth: last insert id: %w", err)
	}

	pair, err := s.issueTokens(ctx, tx, uid, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("auth: commit: %w", err)
	}
	return &authResult{
		User:         &User{ID: uid, Email: d.Email, Nickname: d.Nickname, CreatedAt: rfc3339(now)},
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, nil
}

// login 邮箱查用户 → argon2 校验 → 签发新对。两类失败同一响应（40103）。
func (s *service) login(ctx context.Context, d loginData) (*authResult, error) {
	var (
		uid                     int64
		email, pwHash, nickname string
		createdAt               time.Time
	)
	err := s.db.QueryRowContext(ctx, sqlSelectUserByEmail, d.Email).
		Scan(&uid, &email, &pwHash, &nickname, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		dummyVerify(d.Password) // 时间侧信道对齐：未注册邮箱同样付一次 argon2 成本
		return nil, errBadCredentials()
	}
	if err != nil {
		return nil, fmt.Errorf("auth: select user: %w", err)
	}
	if !verifyPassword(pwHash, d.Password) {
		return nil, errBadCredentials()
	}

	now := s.now().UTC().Truncate(time.Millisecond)
	pair, err := s.issueTokens(ctx, s.db, uid, now)
	if err != nil {
		return nil, err
	}
	return &authResult{
		User:         &User{ID: uid, Email: email, Nickname: nickname, CreatedAt: rfc3339(createdAt)},
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
	}, nil
}

// refresh 旋转：校验旧 token（存在 / 未吊销 / 未过期）→ 立即吊销 → 同事务签发新对。
// 吊销与新发一个事务，杜绝「旧的已死新的没发出去」的中间态。
func (s *service) refresh(ctx context.Context, rawToken string) (*tokenPair, error) {
	now := s.now().UTC().Truncate(time.Millisecond)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("auth: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var (
		id, uid   int64
		expiresAt time.Time
		revokedAt sql.NullTime
	)
	err = tx.QueryRowContext(ctx, sqlSelectRefreshForUpdate, hashToken(rawToken)).
		Scan(&id, &uid, &expiresAt, &revokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errRefreshInvalid()
	}
	if err != nil {
		return nil, fmt.Errorf("auth: select refresh: %w", err)
	}
	if revokedAt.Valid || !now.Before(expiresAt) {
		return nil, errRefreshInvalid()
	}

	if _, err := tx.ExecContext(ctx, sqlRevokeRefreshByID, now, id); err != nil {
		return nil, fmt.Errorf("auth: revoke refresh: %w", err)
	}
	pair, err := s.issueTokens(ctx, tx, uid, now)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("auth: commit: %w", err)
	}
	return pair, nil
}

// logout 吊销指定 refresh；幂等：未知 / 已吊销 token 一样成功（契约 §2 → data: null）。
func (s *service) logout(ctx context.Context, rawToken string) error {
	now := s.now().UTC().Truncate(time.Millisecond)
	if _, err := s.db.ExecContext(ctx, sqlRevokeRefreshByHash, now, hashToken(rawToken)); err != nil {
		return fmt.Errorf("auth: logout revoke: %w", err)
	}
	return nil
}

// execer *sql.DB 与 *sql.Tx 共用的最小写接口（issueTokens 两处复用）。
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// issueTokens 生成随机 refresh（DB 只存 sha256）+ 签发 access JWT。
func (s *service) issueTokens(ctx context.Context, ex execer, uid int64, now time.Time) (*tokenPair, error) {
	raw, err := newRefreshToken()
	if err != nil {
		return nil, err
	}
	if _, err := ex.ExecContext(ctx, sqlInsertRefresh, uid, hashToken(raw), now.Add(s.refreshTTL), now); err != nil {
		// token_hash 唯一键撞车 = 256-bit 随机碰撞，概率可忽略；按内部错误处理。
		return nil, fmt.Errorf("auth: insert refresh: %w", err)
	}
	access, err := s.tm.Sign(uid, s.accessTTL)
	if err != nil {
		return nil, fmt.Errorf("auth: sign access: %w", err)
	}
	return &tokenPair{AccessToken: access, RefreshToken: raw}, nil
}

// newRefreshToken 256-bit crypto/rand，hex 编码（64 字符）。
func newRefreshToken() (string, error) {
	var b [refreshTokenBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("auth: generate refresh token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// hashToken refresh token 的落库形态：sha256 hex（refresh_tokens.token_hash CHAR(64)）。
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
