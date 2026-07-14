package vision

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
)

// EnvAPIKey 识票所需的 LLM API key 环境变量（与项目其他配置同 PIAOJU_ 前缀）。
// 未设置时 Routes 照常返回可挂载的路由，调用端点回 50001，服务不受影响。
const EnvAPIKey = "PIAOJU_LLM_API_KEY"

// Routes 契约 §6.1 POST /api/v1/tickets/recognize。主线程挂在 Auth 组下：
//
//	sec.Mount("/tickets/recognize", vision.Routes(conn, cfg.UploadDir))
//
// uploadDir 必须与 upload.Routes 传同一个目录（读的是 upload 落盘的原图）。
func Routes(db *sql.DB, uploadDir string) chi.Router {
	s := &service{db: db, dir: uploadDir}
	if key := os.Getenv(EnvAPIKey); key != "" {
		s.llm = newClaudeClient(key)
	}
	return routes(s)
}

// routes 供单测注入 fake LLM 的 service。
func routes(s *service) chi.Router {
	h := &handler{svc: s}
	r := chi.NewRouter()
	r.Post("/", h.recognize)
	return r
}

type handler struct {
	svc *service
}

type recognizeReq struct {
	AttachmentID int64 `json:"attachmentId"`
}

func (h *handler) recognize(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UID(r.Context())
	if uid <= 0 { // 防御：路由未挂 Auth
		httpx.Err(w, apperr.New(apperr.CodeTokenExpired, "unauthorized"))
		return
	}

	var in recognizeReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httpx.Err(w, apperr.New(apperr.CodeInvalidParam, "invalid json body"))
		return
	}

	draft, err := h.svc.Recognize(r.Context(), uid, in.AttachmentID)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, draft)
}
