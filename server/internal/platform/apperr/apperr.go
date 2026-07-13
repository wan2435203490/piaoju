// Package apperr 业务错误。code 与 docs/PROTOCOL.md §1 错误码表一一对应，
// 新增码必须先改契约（仅主线程可改）再在此登记。
//
// 约定（piaoju-conventions §3）：service 层返回 apperr.New(code, msg)，
// 由 httpx.Err 统一转响应信封；handler 内禁止手写错误 JSON。
package apperr

import (
	"fmt"
	"net/http"
)

// docs/PROTOCOL.md §1 错误码表。
const (
	CodeOK              = 0     // ok
	CodeInvalidParam    = 40001 // 参数校验失败
	CodeUnsupportedEnum = 40002 // 不支持的枚举值
	CodeTokenExpired    = 40101 // access token 过期/无效
	CodeRefreshInvalid  = 40102 // refresh token 无效/已吊销
	CodeBadCredentials  = 40103 // 邮箱或密码错误
	CodeNotFound        = 40401 // 资源不存在或无权访问
	CodeEmailTaken      = 40901 // 邮箱已注册
	CodeConflict        = 40902 // 幂等冲突（同 id 不同内容且 updatedAt 更旧）
	CodeUploadTooLarge  = 41301 // 上传文件超限（>10MB 或非图片）
	CodeInternal        = 50000 // 服务端错误
)

// Error 携带业务码的错误，message 会原样出现在响应信封里（勿放内部细节）。
type Error struct {
	Code int
	Msg  string
}

// New 构造业务错误。code 必须来自 PROTOCOL.md §1 码表。
func New(code int, msg string) *Error {
	return &Error{Code: code, Msg: msg}
}

func (e *Error) Error() string {
	return fmt.Sprintf("apperr %d: %s", e.Code, e.Msg)
}

// HTTPStatus 由业务码前三位推导 HTTP 状态码：
// 40001→400、40101→401、40401→404、40901→409、41301→413、50000→500。
func (e *Error) HTTPStatus() int {
	s := e.Code / 100
	if s < 100 || s > 599 {
		return http.StatusInternalServerError
	}
	return s
}
