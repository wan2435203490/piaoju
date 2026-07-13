package auth

import (
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
	"piaoju/internal/platform/token"
)

// Routes 契约 §2 /api/v1/auth。四个端点全部公开，主线程挂在「公开路由挂载点」
// （router.go 内、Auth 组之外）：
//
//	api.Mount("/auth", auth.Routes(conn, tm, cfg.AccessTTL, cfg.RefreshTTL))
//
// tm 必须与 middleware.Auth 用同一个 *token.Manager（同一 JWT 密钥）。
func Routes(db *sql.DB, tm *token.Manager, accessTTL, refreshTTL time.Duration) chi.Router {
	h := &handler{
		svc:     &service{db: db, tm: tm, accessTTL: accessTTL, refreshTTL: refreshTTL, now: time.Now},
		limiter: newRateLimiter(authRateMax, authRateWindow, time.Now),
	}
	r := chi.NewRouter()
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
	r.Post("/logout", h.logout)
	return r
}

type handler struct {
	svc     *service
	limiter *rateLimiter // login/register/refresh 各自独立配额（key 按端点前缀隔离），5/min/IP
}

// throttle 端点限流：超限时写出错误响应并返回 false。key 带端点前缀，
// 各端点配额互不占用（login 被刷不影响正常 refresh）。
// 超限暂用 40001——契约码表无 429 段位（与 login 既有行为一致）。
func (h *handler) throttle(w http.ResponseWriter, r *http.Request, endpoint string) bool {
	if h.limiter.allow(endpoint + ":" + clientIP(r)) {
		return true
	}
	httpx.Err(w, apperr.New(apperr.CodeInvalidParam, "too many "+endpoint+" attempts, please retry in a minute"))
	return false
}

func decodeBody(r *http.Request) (body, error) {
	var in body
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		return in, apperr.New(apperr.CodeInvalidParam, "invalid json body")
	}
	return in, nil
}

// register POST /register {email,password(≥8),nickname} → {user,accessToken,refreshToken}。
// 限流在最前：argon2id 哈希（~19MiB/~15ms）在任何 DB 检查前无条件执行，且 40901
// 是无速率约束的邮箱枚举通道——不限流即 CPU/内存放大 + 批量灌库。
func (h *handler) register(w http.ResponseWriter, r *http.Request) {
	if !h.throttle(w, r, "register") {
		return
	}
	in, err := decodeBody(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	d, err := parseRegister(in)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	res, err := h.svc.register(r.Context(), d)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, res)
}

// login POST /login {email,password} → 同 register 响应形状；错误恒 40103。
// 限流在最前（含校验失败的请求都计数）。
func (h *handler) login(w http.ResponseWriter, r *http.Request) {
	if !h.throttle(w, r, "login") {
		return
	}
	in, err := decodeBody(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	d, err := parseLogin(in)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	res, err := h.svc.login(r.Context(), d)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, res)
}

// refresh POST /refresh {refreshToken} → 新 token 对；旧 refresh 立即吊销（旋转）。
// 限流防离线爆破 refresh token 与无认证的 DB 压力放大。
func (h *handler) refresh(w http.ResponseWriter, r *http.Request) {
	if !h.throttle(w, r, "refresh") {
		return
	}
	in, err := decodeBody(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	raw, err := parseRefreshToken(in)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	pair, err := h.svc.refresh(r.Context(), raw)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, pair)
}

// logout POST /logout {refreshToken} → data: null（幂等）。
func (h *handler) logout(w http.ResponseWriter, r *http.Request) {
	in, err := decodeBody(r)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	raw, err := parseRefreshToken(in)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	if err := h.svc.logout(r.Context(), raw); err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, nil)
}

// clientIP 限流 key：RemoteAddr 的 host 部分。
// 刻意不读 X-Forwarded-For——无可信反代配置时该头可伪造以绕过限流；
// 将来部署到反代之后，在此处按可信跳数解析。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
