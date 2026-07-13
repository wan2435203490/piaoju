// Package auth 契约 §2：注册 / 登录 / 刷新 / 登出。
//
// access token = JWT（platform/token 签发，middleware.Auth 验签）；
// refresh token = 256-bit 随机值（hex 64 字符），DB 只存 sha256 哈希
// （refresh_tokens.token_hash CHAR(64)），刷新旋转时旧 token 立即吊销。
//
// 本模块是唯一的公开路由组（不挂 Auth 中间件）；users / refresh_tokens
// 表只归本模块访问。
package auth

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"piaoju/internal/platform/apperr"
)

// 字段上限（对齐 migrations/0001 列宽；password 上限为业务约束，防 argon2 成本滥用）。
const (
	maxEmailLen    = 191 // users.email VARCHAR(191)
	maxNicknameLen = 64  // users.nickname VARCHAR(64)
	minPasswordLen = 8   // 契约 §2 password(≥8)
	maxPasswordLen = 128
)

// emailRe 宽松格式校验：非空 local@domain.tld，不做 RFC5322 全量解析。
var emailRe = regexp.MustCompile(`^[^\s@]+@[^\s@]+\.[^\s@]+$`)

// User 契约 §2 User 对象。
type User struct {
	ID        int64  `json:"id"`
	Email     string `json:"email"`
	Nickname  string `json:"nickname"`
	CreatedAt string `json:"createdAt"`
}

// body 四个端点共用解码结构：指针区分「未提供」与零值。
type body struct {
	Email        *string `json:"email"`
	Password     *string `json:"password"`
	Nickname     *string `json:"nickname"`
	RefreshToken *string `json:"refreshToken"`
}

// authResult register / login 响应（契约 §2）。
type authResult struct {
	User         *User  `json:"user"`
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// tokenPair refresh 响应（契约 §2）。
type tokenPair struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
}

// registerData 校验通过的注册载荷（email 已 trim + 小写归一）。
type registerData struct {
	Email    string
	Password string
	Nickname string
}

// loginData 校验通过的登录载荷。
type loginData struct {
	Email    string
	Password string
}

func badParam(format string, a ...any) error {
	return apperr.New(apperr.CodeInvalidParam, fmt.Sprintf(format, a...))
}

// rfc3339 契约时间格式：RFC3339 UTC。
func rfc3339(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// parseRegister 校验注册载荷；email 小写归一（DB collation 也不区分大小写，双保险）。
// 密码不 trim：首尾空白也是密码的一部分。
func parseRegister(in body) (registerData, error) {
	var d registerData

	if in.Email == nil {
		return d, badParam("email is required")
	}
	email := strings.ToLower(strings.TrimSpace(*in.Email))
	if email == "" {
		return d, badParam("email is required")
	}
	if utf8.RuneCountInString(email) > maxEmailLen {
		return d, badParam("email too long (max %d chars)", maxEmailLen)
	}
	if !emailRe.MatchString(email) {
		return d, badParam("email format is invalid")
	}
	d.Email = email

	if in.Password == nil {
		return d, badParam("password is required")
	}
	n := utf8.RuneCountInString(*in.Password)
	if n < minPasswordLen {
		return d, badParam("password must be at least %d characters", minPasswordLen)
	}
	if n > maxPasswordLen {
		return d, badParam("password too long (max %d chars)", maxPasswordLen)
	}
	d.Password = *in.Password

	if in.Nickname == nil {
		return d, badParam("nickname is required")
	}
	nick := strings.TrimSpace(*in.Nickname)
	if nick == "" {
		return d, badParam("nickname must not be empty")
	}
	if utf8.RuneCountInString(nick) > maxNicknameLen {
		return d, badParam("nickname too long (max %d chars)", maxNicknameLen)
	}
	d.Nickname = nick
	return d, nil
}

// parseLogin 只做必填检查 + email 归一；格式/长度不合法的 email 走正常查询路径
// 得到统一的 40103，不暴露「校验未通过 vs 账号不存在」的差异。
func parseLogin(in body) (loginData, error) {
	var d loginData
	if in.Email == nil || strings.TrimSpace(*in.Email) == "" {
		return d, badParam("email is required")
	}
	if in.Password == nil || *in.Password == "" {
		return d, badParam("password is required")
	}
	d.Email = strings.ToLower(strings.TrimSpace(*in.Email))
	d.Password = *in.Password
	return d, nil
}

// parseRefreshToken refresh / logout 共用：缺失 → 40001；
// 格式错误不预判，交给哈希查询统一得 40102（refresh）或幂等成功（logout）。
func parseRefreshToken(in body) (string, error) {
	if in.RefreshToken == nil {
		return "", badParam("refreshToken is required")
	}
	tok := strings.TrimSpace(*in.RefreshToken)
	if tok == "" {
		return "", badParam("refreshToken is required")
	}
	return tok, nil
}
