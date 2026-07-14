package vision

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"piaoju/internal/platform/apperr"
)

// 契约 §1 新增码（apperr 未登记，本包内用 apperr.New 构造；
// HTTPStatus 由 code/100 推导：42901→429、50001→500，天然可用）。
const (
	codeRateLimited   = 42901 // 识票调用超额/限流
	codeVisionUnready = 50001 // 识票服务未配置（未设 PIAOJU_LLM_API_KEY）
)

// sqlSelectAttachment user_id 隔离：附件不属于当前用户即视为不存在（40401）。
const sqlSelectAttachment = "SELECT file_path FROM attachments WHERE id = ? AND user_id = ?"

type service struct {
	db  *sql.DB
	dir string     // 上传根目录（与 upload.Routes 同一个 PIAOJU_UPLOAD_DIR）
	llm recognizer // nil = 未配置 PIAOJU_LLM_API_KEY → 50001
}

// Recognize 契约 §6.1：attachmentId → 票据草稿（不落库）。
func (s *service) Recognize(ctx context.Context, userID, attachmentID int64) (*Draft, error) {
	if userID <= 0 {
		return nil, apperr.New(apperr.CodeTokenExpired, "unauthorized")
	}
	if attachmentID <= 0 {
		return nil, apperr.New(apperr.CodeInvalidParam, "attachmentId is required")
	}

	// 1. 附件归属校验（user_id 隔离）——先查库，未配 key 也不泄露他人附件的存在性。
	var rel string
	err := s.db.QueryRowContext(ctx, sqlSelectAttachment, attachmentID, userID).Scan(&rel)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, apperr.New(apperr.CodeNotFound, "attachment not found")
	}
	if err != nil {
		return nil, fmt.Errorf("vision: select attachment: %w", err)
	}

	// 2. 识票服务是否可用（可选增强，未配置不影响主流程）。
	if s.llm == nil {
		return nil, apperr.New(codeVisionUnready, "recognize service is not configured")
	}

	// 3. 读磁盘图片（upload 落盘的原图恒为 jpeg）。
	img, err := s.readImage(rel)
	if err != nil {
		return nil, err
	}

	// 4. 调 LLM → 校验归一化。
	out, err := s.llm.recognize(ctx, "image/jpeg", img)
	if err != nil {
		return nil, err
	}
	return out.toDraft()
}

// readImage 从上传根目录读文件；路径来自库中 file_path，仍做穿越双保险（防脏数据）。
func (s *service) readImage(rel string) ([]byte, error) {
	if rel == "" || strings.ContainsAny(rel, "\\\x00") || path.Clean("/"+rel) != "/"+rel {
		return nil, fmt.Errorf("vision: bad attachment path %q", rel)
	}
	absDir, err := filepath.Abs(s.dir)
	if err != nil {
		return nil, fmt.Errorf("vision: resolve upload dir: %w", err)
	}
	full := filepath.Join(absDir, filepath.FromSlash(rel))
	if !strings.HasPrefix(full, absDir+string(os.PathSeparator)) {
		return nil, fmt.Errorf("vision: attachment path escapes upload dir: %q", rel)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		// 库有记录但文件丢了：不是用户输入问题，走 500。
		return nil, fmt.Errorf("vision: read attachment file: %w", err)
	}
	return data, nil
}
