// Package token JWT（HS256）签发与校验。
//
// S1 只负责验签（Auth 中间件）；S2 auth 模块复用同一个 Manager 签发 access token：
//
//	tm := token.NewManager(cfg.JWTSecret)
//	access, err := tm.Sign(user.ID, cfg.AccessTTL)
//
// refresh token 不走 JWT（DB 存哈希、可吊销），由 S2 自行实现。
package token

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	// ErrExpired token 已过期（签名合法）。
	ErrExpired = errors.New("token: expired")
	// ErrInvalid token 非法：签名不符 / 格式错误 / alg 不符 / subject 非法。
	ErrInvalid = errors.New("token: invalid")
)

// Manager 持有 HS256 密钥，并发安全，进程内单例使用。
type Manager struct {
	secret []byte
}

// NewManager 用配置里的 PIAOJU_JWT_SECRET 构造。
func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

// Sign 为 uid 签发 HS256 JWT，有效期 ttl（access token 用 config.AccessTTL）。
func (m *Manager) Sign(uid int64, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   strconv.FormatInt(uid, 10),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	s, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", fmt.Errorf("token: sign: %w", err)
	}
	return s, nil
}

// Verify 校验签名与有效期并返回 uid。
// 过期返回 ErrExpired；其余失败一律 ErrInvalid（不区分细节，防探测）。
func (m *Manager) Verify(tokenStr string) (int64, error) {
	var claims jwt.RegisteredClaims
	_, err := jwt.ParseWithClaims(tokenStr, &claims,
		func(*jwt.Token) (any, error) { return m.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return 0, ErrExpired
		}
		return 0, ErrInvalid
	}
	uid, err := strconv.ParseInt(claims.Subject, 10, 64)
	if err != nil || uid <= 0 {
		return 0, ErrInvalid
	}
	return uid, nil
}
