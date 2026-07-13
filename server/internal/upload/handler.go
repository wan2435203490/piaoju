package upload

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"piaoju/internal/middleware"
	"piaoju/internal/platform/apperr"
	"piaoju/internal/platform/httpx"
)

// Attachment 契约 §5 附件对象。ticket 模块响应内嵌复用此结构。
type Attachment struct {
	ID       int64  `json:"id"`
	URL      string `json:"url"`
	ThumbURL string `json:"thumbUrl"`
	W        int    `json:"w"`
	H        int    `json:"h"`
}

// sqlInsertAttachment ticket_id 先置 NULL，建票时由 ticket 模块绑定（契约 §5 attachmentIds）。
const sqlInsertAttachment = "INSERT INTO attachments (user_id, ticket_id, file_path, thumb_path, w, h, size, created_at) VALUES (?, NULL, ?, ?, ?, ?, ?, ?)"

type service struct {
	db       *sql.DB
	dir      string // 上传根目录（PIAOJU_UPLOAD_DIR，主线程接线）
	maxBytes int64  // 单文件上限（PIAOJU_UPLOAD_MAX_MB，主线程接线）
	secret   string // 签名密钥（与 Serve 一致）
	now      func() time.Time
}

// Routes 契约 §6 POST /api/v1/uploads。主线程挂在 Auth 组下：
//
//	sec.Mount("/uploads", upload.Routes(conn, cfg.UploadDir, cfg.UploadMaxMB, cfg.JWTSecret))
func Routes(db *sql.DB, dir string, maxMB int, secret string) chi.Router {
	s := &service{db: db, dir: dir, maxBytes: int64(maxMB) << 20, secret: secret, now: time.Now}
	r := chi.NewRouter()
	r.Post("/", s.handleUpload)
	return r
}

// handleUpload multipart file → 校验大小/格式 → 压缩+缩略图 → 写盘 → attachments 落库。
func (s *service) handleUpload(w http.ResponseWriter, r *http.Request) {
	uid := middleware.UID(r.Context())
	if uid <= 0 { // 防御：路由未挂 Auth 时不落无主文件
		httpx.Err(w, apperr.New(apperr.CodeTokenExpired, "unauthorized"))
		return
	}

	// 请求体总上限 = 文件上限 + 1MB multipart 头部余量；超限在 FormFile 解析时触发 MaxBytesError。
	r.Body = http.MaxBytesReader(w, r.Body, s.maxBytes+1<<20)
	file, _, err := r.FormFile("file")
	if err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			httpx.Err(w, s.errTooLarge())
			return
		}
		httpx.Err(w, apperr.New(apperr.CodeInvalidParam, `multipart field "file" is required`))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		httpx.Err(w, fmt.Errorf("upload: read multipart file: %w", err))
		return
	}
	if int64(len(data)) > s.maxBytes {
		httpx.Err(w, s.errTooLarge())
		return
	}

	p, err := processImage(data)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	att, err := s.store(r.Context(), uid, p)
	if err != nil {
		httpx.Err(w, err)
		return
	}
	httpx.OK(w, att)
}

func (s *service) errTooLarge() error {
	return apperr.New(apperr.CodeUploadTooLarge, fmt.Sprintf("file exceeds %dMB limit", s.maxBytes>>20))
}

// store 写盘（<dir>/<uid>/<uuid>.jpg 与 ..._thumb.jpg）并落库；任一步失败清理已写文件，不留孤儿。
func (s *service) store(ctx context.Context, uid int64, p *processed) (*Attachment, error) {
	userDir := strconv.FormatInt(uid, 10)
	if err := os.MkdirAll(filepath.Join(s.dir, userDir), 0o755); err != nil {
		return nil, fmt.Errorf("upload: mkdir: %w", err)
	}
	name := newUUID() // 文件名 UUID，不可预测（防遍历枚举）
	rel := userDir + "/" + name + ".jpg"
	thumbRel := userDir + "/" + name + "_thumb.jpg"
	origAbs := filepath.Join(s.dir, filepath.FromSlash(rel))
	thumbAbs := filepath.Join(s.dir, filepath.FromSlash(thumbRel))

	cleanup := func() {
		os.Remove(origAbs)
		os.Remove(thumbAbs)
	}
	if err := os.WriteFile(origAbs, p.orig, 0o644); err != nil {
		cleanup()
		return nil, fmt.Errorf("upload: write original: %w", err)
	}
	if err := os.WriteFile(thumbAbs, p.thumb, 0o644); err != nil {
		cleanup()
		return nil, fmt.Errorf("upload: write thumbnail: %w", err)
	}

	res, err := s.db.ExecContext(ctx, sqlInsertAttachment,
		uid, rel, thumbRel, p.w, p.h, int64(len(p.orig)), s.now().UTC().Truncate(time.Millisecond))
	if err != nil {
		cleanup() // 落库失败回滚文件
		return nil, fmt.Errorf("upload: insert attachment: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("upload: last insert id: %w", err)
	}
	return &Attachment{
		ID:       id,
		URL:      SignURL(s.secret, "/uploads/"+rel, URLTTL),
		ThumbURL: SignURL(s.secret, "/uploads/"+thumbRel, URLTTL),
		W:        p.w,
		H:        p.h,
	}, nil
}

// Serve 契约 §6 GET /uploads/{path}：签名静态文件服务。主线程挂根路由、不带 Auth：
//
//	r.Get("/uploads/*", upload.Serve(cfg.UploadDir, cfg.JWTSecret))
//
// 签名缺失/过期/篡改、路径穿越、文件不存在一律 404 信封（40401），不区分原因。
func Serve(dir, secret string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		notFound := func() { httpx.Err(w, apperr.New(apperr.CodeNotFound, "not found")) }

		q := r.URL.Query()
		if !verifySignedPath(secret, r.URL.Path, q.Get("exp"), q.Get("sig"), time.Now()) {
			notFound()
			return
		}
		rel, ok := safeRelPath(r.URL.Path)
		if !ok {
			notFound()
			return
		}
		// 路径穿越双保险：clean 后必须仍落在 dir 内。
		absDir, err := filepath.Abs(dir)
		if err != nil {
			notFound()
			return
		}
		full := filepath.Join(absDir, filepath.FromSlash(rel))
		if !strings.HasPrefix(full, absDir+string(os.PathSeparator)) {
			notFound()
			return
		}
		fi, err := os.Stat(full)
		if err != nil || fi.IsDir() {
			notFound()
			return
		}
		http.ServeFile(w, r, full)
	}
}

// safeRelPath 从 URL path 提取 dir 下相对路径。
// 拒绝：非 /uploads/ 前缀、反斜杠/NUL、任何 ".."/"."/空段（path.Clean 后必须与原样一致）。
func safeRelPath(urlPath string) (string, bool) {
	const prefix = "/uploads/"
	if !strings.HasPrefix(urlPath, prefix) {
		return "", false
	}
	rel := strings.TrimPrefix(urlPath, prefix)
	if rel == "" || strings.ContainsAny(rel, "\\\x00") {
		return "", false
	}
	if path.Clean("/"+rel) != "/"+rel { // 含 ..、//、./、尾斜杠等一律拒绝
		return "", false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "", false
		}
	}
	return rel, true
}

// newUUID 生成 UUIDv4（crypto/rand，不引第三方库）。
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("upload: crypto/rand unavailable: " + err.Error()) // 系统级故障，无降级路径
	}
	b[6] = b[6]&0x0f | 0x40 // version 4
	b[8] = b[8]&0x3f | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
