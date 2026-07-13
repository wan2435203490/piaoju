package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
)

// Recover 捕获下游 panic：slog 打详情 + 堆栈，客户端只见 50000 信封。
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			p := recover()
			if p == nil {
				return
			}
			if p == http.ErrAbortHandler { // net/http 约定的中止哨兵，原样上抛
				panic(p)
			}
			slog.Error("panic recovered",
				"panic", fmt.Sprint(p),
				"method", r.Method,
				"path", r.URL.Path,
				"stack", string(debug.Stack()),
			)
			httpx.Err(w, apperr.New(apperr.CodeInternal, "internal server error"))
		}()
		next.ServeHTTP(w, r)
	})
}
