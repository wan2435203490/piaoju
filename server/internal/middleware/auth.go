// Package middleware 全局 HTTP 中间件：Auth / Recover / CORS / RequestLog。
package middleware

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
	"piaoju/internal/platform/token"
)

// ctxKeyUID uid 的 context key（非导出类型防碰撞）。
type ctxKeyUID struct{}

// Auth 解析 `Authorization: Bearer <access_token>` 并验签。
// 失败统一返回 40101 信封（PROTOCOL.md §1）；成功把 uid 注入 context，
// 下游 handler 用 middleware.UID(r.Context()) 取用。
func Auth(tm *token.Manager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr := bearerToken(r.Header.Get("Authorization"))
			if tokenStr == "" {
				httpx.Err(w, apperr.New(apperr.CodeTokenExpired, "missing bearer token"))
				return
			}
			uid, err := tm.Verify(tokenStr)
			if err != nil {
				msg := "invalid access token"
				if errors.Is(err, token.ErrExpired) {
					msg = "token expired"
				}
				httpx.Err(w, apperr.New(apperr.CodeTokenExpired, msg))
				return
			}
			ctx := context.WithValue(r.Context(), ctxKeyUID{}, uid)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// UID 返回 Auth 注入的用户 ID；context 里没有（路由忘挂 Auth）时返回 0。
// 所有 DB 查询必须带此 user_id 条件，漏带即安全 bug（piaoju-conventions §2）。
func UID(ctx context.Context) int64 {
	uid, _ := ctx.Value(ctxKeyUID{}).(int64)
	return uid
}

// bearerToken 从 Authorization 头提取 token；scheme 大小写不敏感（RFC 6750）。
func bearerToken(header string) string {
	const prefix = "bearer "
	if len(header) > len(prefix) && strings.EqualFold(header[:len(prefix)], prefix) {
		return strings.TrimSpace(header[len(prefix):])
	}
	return ""
}
