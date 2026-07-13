// Package httpx 统一响应信封 {code,message,data}（piaoju-conventions §2）。
// 所有 HTTP 响应必须经 OK / Err 写出，保证成功与失败结构固定。
package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"piaoju/internal/platform/apperr"
)

// Envelope 响应信封。成功 code=0、失败 data 恒为 null。
type Envelope struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

// OK 写成功信封：HTTP 200 + {"code":0,"message":"ok","data":...}。
func OK(w http.ResponseWriter, data any) {
	write(w, http.StatusOK, Envelope{Code: apperr.CodeOK, Message: "ok", Data: data})
}

// Err 写失败信封。识别 *apperr.Error（含 errors.As 解包）时用其 code/msg 与推导状态码；
// 其余错误一律 50000 且不向客户端泄漏内部信息，详情只进日志。
func Err(w http.ResponseWriter, err error) {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		write(w, ae.HTTPStatus(), Envelope{Code: ae.Code, Message: ae.Msg, Data: nil})
		return
	}
	slog.Error("unhandled internal error", "err", err)
	write(w, http.StatusInternalServerError,
		Envelope{Code: apperr.CodeInternal, Message: "internal server error", Data: nil})
}

func write(w http.ResponseWriter, status int, env Envelope) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(env); err != nil {
		slog.Error("httpx: encode envelope", "err", err)
	}
}
