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
	"sync"

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

	// 准入控制（懒初始化，见 throttle.go）：per-uid 配额 + 并发信号量，
	// 防单个用户烧光共享 LLM 额度并触发上游全局 429。
	limiterOnce sync.Once
	perMin      *slidingLimiter
	perDay      *slidingLimiter
	sem         chan struct{}
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

	// 3. 准入控制：识票每次调用 = 一次付费 Opus 请求且全体共享同一 API key，
	//    必须在发起上游调用前按用户计量。超额直接 42901，不烧共享额度、不触发上游 429。
	//    先查分钟窗（更常命中，超限即短路，不误记日窗）。
	perMin, perDay, sem := s.admit()
	key := uidKey(userID)
	if !perMin.allow(key) || !perDay.allow(key) {
		return nil, apperr.New(codeRateLimited, "recognize quota exceeded, retry later")
	}

	// 4. 读磁盘图片（upload 落盘的原图恒为 jpeg）。
	img, err := s.readImage(rel)
	if err != nil {
		return nil, err
	}

	// 5. 并发信号量（非阻塞）：全局在途上游调用封顶，防几百并发各占一个 goroutine/连接
	//    最长 60s 打满服务；已满即 42901，不排队堆积。
	select {
	case sem <- struct{}{}:
		defer func() { <-sem }()
	default:
		return nil, apperr.New(codeRateLimited, "recognize service is busy, retry later")
	}

	// 6. 调 LLM → 校验归一化。
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
