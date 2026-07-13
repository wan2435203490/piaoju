package sync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/upload"
)

// SQL 常量（测试用 regexp.QuoteMeta 复用，防实现/断言漂移）。
// 所有语句一律带 user_id 条件（conventions §2：漏带即安全 bug）。
// 跨表访问的豁免范围见 types.go 包注释。
const (
	// ── pull：keyset 扫描（updated_at, id）升序，含墓碑行（deleted_at 不过滤）──
	sqlSelectTxJoin = "SELECT tx.id, tx.amount_cents, tx.direction, tx.category_id, tx.note, tx.occurred_at, tx.payment_method, tx.created_at, tx.updated_at, tx.deleted_at, tk.id" +
		" FROM transactions tx LEFT JOIN tickets tk ON tk.transaction_id = tx.id AND tk.user_id = tx.user_id AND tk.deleted_at IS NULL"

	sqlPullTxAll = sqlSelectTxJoin +
		" WHERE tx.user_id = ? ORDER BY tx.updated_at, tx.id LIMIT ?"
	sqlPullTxSince = sqlSelectTxJoin +
		" WHERE tx.user_id = ? AND (tx.updated_at > ? OR (tx.updated_at = ? AND tx.id > ?))" +
		" ORDER BY tx.updated_at, tx.id LIMIT ?"

	sqlSelectTicketJoin = "SELECT t.id, t.transaction_id, t.kind, t.title, t.venue, t.event_time, t.seat, t.extra, t.rating, t.memo, t.created_at, t.updated_at, t.deleted_at, tx.amount_cents, tx.category_id, tx.payment_method" +
		" FROM tickets t JOIN transactions tx ON tx.id = t.transaction_id AND tx.user_id = t.user_id"

	sqlPullTicketsAll = sqlSelectTicketJoin +
		" WHERE t.user_id = ? ORDER BY t.updated_at, t.id LIMIT ?"
	sqlPullTicketsSince = sqlSelectTicketJoin +
		" WHERE t.user_id = ? AND (t.updated_at > ? OR (t.updated_at = ? AND t.id > ?))" +
		" ORDER BY t.updated_at, t.id LIMIT ?"

	// categories 无 updated_at 列（契约 §9），无法 keyset 增量 → 全量下发（含墓碑）。
	sqlPullCategories = "SELECT id, user_id, name, icon, kind, sort, deleted_at FROM categories" +
		" WHERE user_id IS NULL OR user_id = ? ORDER BY kind, sort, id"

	// ── push：幂等 upsert / 软删墓碑 ──
	sqlSelectTxMeta = "SELECT tx.direction, tx.created_at, tx.updated_at, tx.deleted_at, tk.id" +
		" FROM transactions tx LEFT JOIN tickets tk ON tk.transaction_id = tx.id AND tk.user_id = tx.user_id AND tk.deleted_at IS NULL" +
		" WHERE tx.id = ? AND tx.user_id = ? FOR UPDATE"

	sqlInsertTx = "INSERT INTO transactions (id, user_id, amount_cents, direction, category_id, note, occurred_at, payment_method, created_at, updated_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	// LWW upsert：整体覆盖并清墓碑（复活软删行）。direction 建后不可改，不在 SET 内。
	sqlUpsertTx = "UPDATE transactions SET amount_cents = ?, category_id = ?, note = ?, occurred_at = ?, payment_method = ?, updated_at = ?, deleted_at = NULL" +
		" WHERE id = ? AND user_id = ?"

	sqlSoftDeleteTx = "UPDATE transactions SET updated_at = ?, deleted_at = ? WHERE id = ? AND user_id = ?"

	sqlSelectTicketMeta = "SELECT transaction_id, created_at, updated_at, deleted_at FROM tickets WHERE id = ? AND user_id = ? FOR UPDATE"

	sqlInsertTicket = "INSERT INTO tickets (id, user_id, transaction_id, kind, title, venue, event_time, seat, extra, rating, memo, created_at, updated_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	// 票↔交易联动（契约 §5）：建票/重放同事务写交易，note = title 快照。
	sqlUpsertTicketTx = "UPDATE transactions SET amount_cents = ?, category_id = ?, payment_method = ?, note = ?, occurred_at = ?, updated_at = ?, deleted_at = NULL" +
		" WHERE id = ? AND user_id = ?"
	sqlUpsertTicket = "UPDATE tickets SET kind = ?, title = ?, venue = ?, event_time = ?, seat = ?, extra = ?, rating = ?, memo = ?, updated_at = ?, deleted_at = NULL" +
		" WHERE id = ? AND user_id = ?"

	sqlSoftDeleteTicket = "UPDATE tickets SET updated_at = ?, deleted_at = ? WHERE id = ? AND user_id = ?"

	sqlUnbindAllAttachments = "UPDATE attachments SET ticket_id = NULL WHERE user_id = ? AND ticket_id = ?"

	mysqlErrDuplicateEntry = 1062
)

// 动态 IN(...) 语句模板（%s 为占位符串），测试用同款 helper 构造。
const (
	sqlListAttachmentsIn = "SELECT id, ticket_id, file_path, thumb_path, w, h FROM attachments WHERE user_id = ? AND ticket_id IN (%s) ORDER BY ticket_id, id"
	// 绑定条件里带 (ticket_id IS NULL OR ticket_id = ?)：本人的、且未被别的票占用的附件才绑，
	// 越权/抢绑一律静默跳过（sync 是批量补写路径，单张附件绑不上不该让整条 change 失败）。
	sqlBindAttachmentsIn = "UPDATE attachments SET ticket_id = ? WHERE user_id = ? AND id IN (%s) AND (ticket_id IS NULL OR ticket_id = ?)"
	sqlUnbindOthersNotIn = "UPDATE attachments SET ticket_id = NULL WHERE user_id = ? AND ticket_id = ? AND id NOT IN (%s)"
)

type service struct {
	db     *sql.DB
	secret string           // upload 签名密钥（pull 渲染附件 url/thumbUrl）
	now    func() time.Time // 测试注入固定时钟
}

// ── push ────────────────────────────────────────────────────────────────────

// errStale 内部哨兵：LWW 判定客户端版本更旧 → status=stale（不写入，服务端版本随 pull 下发）。
var errStale = errors.New("sync: stale write")

// push 逐条应用变更。每条 change 独立 DB 事务，单条失败（校验/冲突/DB 错误）只体现在
// 自己的 result 里，不影响其余条目（契约 §8）。
func (s *service) push(ctx context.Context, uid int64, raws []rawChange) *pushResult {
	results := make([]changeResult, 0, len(raws))
	for _, raw := range raws {
		c, err := parseChange(raw)
		if err != nil {
			results = append(results, changeResult{
				ID: bestEffortID(raw.Payload), Status: statusError, Code: codeOf(err),
			})
			continue
		}
		switch err := s.apply(ctx, uid, c); {
		case err == nil:
			results = append(results, changeResult{ID: c.ID, Status: statusApplied, Code: apperr.CodeOK})
		case errors.Is(err, errStale):
			results = append(results, changeResult{ID: c.ID, Status: statusStale, Code: apperr.CodeConflict})
		default:
			results = append(results, changeResult{ID: c.ID, Status: statusError, Code: codeOf(err)})
		}
	}
	return &pushResult{Results: results}
}

// codeOf 取业务码；非 apperr（DB 故障等）一律 50000，内部细节只进日志不回客户端。
func codeOf(err error) int {
	var ae *apperr.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	slog.Error("sync: push change failed", "err", err)
	return apperr.CodeInternal
}

func (s *service) apply(ctx context.Context, uid int64, c change) error {
	switch {
	case c.Entity == "transaction" && c.Op == "upsert":
		return s.upsertTx(ctx, uid, *c.Tx, c.ClientUpdated)
	case c.Entity == "transaction" && c.Op == "delete":
		return s.deleteTx(ctx, uid, c.ID, c.ClientUpdated)
	case c.Entity == "ticket" && c.Op == "upsert":
		return s.upsertTicket(ctx, uid, *c.Ticket, c.ClientUpdated)
	default:
		return s.deleteTicket(ctx, uid, c.ID, c.ClientUpdated)
	}
}

// upsertTx 幂等 upsert 交易：
//   - 全新 id → INSERT；主键被他人占用 → 40902（不泄漏归属）
//   - 已存在 → LWW：clientUpdatedAt < 服务端 updated_at → stale；否则整体覆盖（含复活软删行）
//   - direction 建后不可改（与 transaction 模块同规则）→ 40001
func (s *service) upsertTx(ctx context.Context, uid int64, d txData, clientUpdated time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sync: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // commit 后为 ErrTxDone no-op

	now := s.now().UTC().Truncate(time.Millisecond)

	var (
		direction string
		createdAt time.Time
		updatedAt time.Time
		deletedAt sql.NullTime
		ticketID  sql.NullString
	)
	err = tx.QueryRowContext(ctx, sqlSelectTxMeta, d.ID, uid).
		Scan(&direction, &createdAt, &updatedAt, &deletedAt, &ticketID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, sqlInsertTx,
			d.ID, uid, d.AmountCents, d.Direction, d.CategoryID, d.Note, d.OccurredAt, d.PaymentMethod, now, now); err != nil {
			return mapInsertErr(err, "sync: insert transaction")
		}
		return commit(tx, "sync: commit")
	case err != nil:
		return fmt.Errorf("sync: transaction meta: %w", err)
	}

	if clientUpdated.Before(updatedAt) {
		return errStale
	}
	if d.Direction != direction {
		return badParam("direction cannot be changed")
	}
	if _, err := tx.ExecContext(ctx, sqlUpsertTx,
		d.AmountCents, d.CategoryID, d.Note, d.OccurredAt, d.PaymentMethod, now, d.ID, uid); err != nil {
		return fmt.Errorf("sync: upsert transaction: %w", err)
	}
	return commit(tx, "sync: commit")
}

// deleteTx 软删墓碑（bump updated_at 供 pull 下发）。
// 不存在 / 已是墓碑 → applied（幂等 no-op）；票据关联的交易必须从票夹删（契约 v1.1）→ 40001。
func (s *service) deleteTx(ctx context.Context, uid int64, id string, clientUpdated time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sync: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var (
		direction string
		createdAt time.Time
		updatedAt time.Time
		deletedAt sql.NullTime
		ticketID  sql.NullString
	)
	err = tx.QueryRowContext(ctx, sqlSelectTxMeta, id, uid).
		Scan(&direction, &createdAt, &updatedAt, &deletedAt, &ticketID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // 服务端本就没有这行：删除幂等成功
	}
	if err != nil {
		return fmt.Errorf("sync: transaction meta: %w", err)
	}
	if deletedAt.Valid {
		return nil // 已是墓碑：不重复 bump updated_at（避免游标抖动重复下发）
	}
	if clientUpdated.Before(updatedAt) {
		return errStale
	}
	if ticketID.Valid {
		return badParam("请从票夹删除该票据")
	}

	now := s.now().UTC().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, sqlSoftDeleteTx, now, now, id, uid); err != nil {
		return fmt.Errorf("sync: soft delete transaction: %w", err)
	}
	return commit(tx, "sync: commit")
}

// upsertTicket 幂等 upsert 票据，同事务维持票↔交易联动一致性（契约 §5）：
//   - 全新 id → 先 INSERT transactions（id = payload.transactionId 客户端 UUID、direction 恒 expense、
//     note = title 快照）再 INSERT tickets
//   - 已存在 → LWW stale 判定后同事务覆盖 tickets + 其关联 transactions；联动交易主键以库中
//     已存在的 transaction_id 为准，不被 payload 覆盖（客户端重放换 id 会导致交易分裂）
//   - attachmentIds 提供时按全量集合语义重绑（未提供则不动附件）
func (s *service) upsertTicket(ctx context.Context, uid int64, d ticketData, clientUpdated time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sync: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := s.now().UTC().Truncate(time.Millisecond)
	extraJSON, err := json.Marshal(d.Extra)
	if err != nil {
		return fmt.Errorf("sync: marshal extra: %w", err)
	}

	var (
		existingTxID string
		createdAt    time.Time
		updatedAt    time.Time
		deletedAt    sql.NullTime
	)
	err = tx.QueryRowContext(ctx, sqlSelectTicketMeta, d.ID, uid).
		Scan(&existingTxID, &createdAt, &updatedAt, &deletedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		txID := d.TransactionID // 客户端生成（契约 §5 v1.2）；被占用 → 主键冲突 → 40902
		if _, err := tx.ExecContext(ctx, sqlInsertTx,
			txID, uid, d.AmountCents, "expense", d.CategoryID, d.Title, d.OccurredAt, d.PaymentMethod, now, now); err != nil {
			return mapInsertErr(err, "sync: insert ticket transaction")
		}
		if _, err := tx.ExecContext(ctx, sqlInsertTicket,
			d.ID, uid, txID, d.Kind, d.Title, d.Venue, d.EventTime, d.Seat, extraJSON, d.Rating, d.Memo, now, now); err != nil {
			return mapInsertErr(err, "sync: insert ticket")
		}
		if err := s.rebindAttachments(ctx, tx, uid, d.ID, d.AttachmentIDs); err != nil {
			return err
		}
		return commit(tx, "sync: commit")
	case err != nil:
		return fmt.Errorf("sync: ticket meta: %w", err)
	}

	if clientUpdated.Before(updatedAt) {
		return errStale
	}
	if _, err := tx.ExecContext(ctx, sqlUpsertTicketTx,
		d.AmountCents, d.CategoryID, d.PaymentMethod, d.Title, d.OccurredAt, now, existingTxID, uid); err != nil {
		return fmt.Errorf("sync: upsert ticket transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, sqlUpsertTicket,
		d.Kind, d.Title, d.Venue, d.EventTime, d.Seat, extraJSON, d.Rating, d.Memo, now, d.ID, uid); err != nil {
		return fmt.Errorf("sync: upsert ticket: %w", err)
	}
	if err := s.rebindAttachments(ctx, tx, uid, d.ID, d.AttachmentIDs); err != nil {
		return err
	}
	return commit(tx, "sync: commit")
}

// deleteTicket 同事务双软删（票 + 关联交易，契约 §5）。不存在 / 已是墓碑 → applied（幂等 no-op）。
func (s *service) deleteTicket(ctx context.Context, uid int64, id string, clientUpdated time.Time) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("sync: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var (
		txID      string
		createdAt time.Time
		updatedAt time.Time
		deletedAt sql.NullTime
	)
	err = tx.QueryRowContext(ctx, sqlSelectTicketMeta, id, uid).
		Scan(&txID, &createdAt, &updatedAt, &deletedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("sync: ticket meta: %w", err)
	}
	if deletedAt.Valid {
		return nil
	}
	if clientUpdated.Before(updatedAt) {
		return errStale
	}

	now := s.now().UTC().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, sqlSoftDeleteTicket, now, now, id, uid); err != nil {
		return fmt.Errorf("sync: soft delete ticket: %w", err)
	}
	if _, err := tx.ExecContext(ctx, sqlSoftDeleteTx, now, now, txID, uid); err != nil {
		return fmt.Errorf("sync: soft delete ticket transaction: %w", err)
	}
	return commit(tx, "sync: commit")
}

// rebindAttachments 全量集合语义：ids 为 nil = 未提供，不动附件；空列表 = 解绑全部。
func (s *service) rebindAttachments(ctx context.Context, tx *sql.Tx, uid int64, ticketID string, ids *[]int64) error {
	if ids == nil {
		return nil
	}
	list := *ids
	if len(list) == 0 {
		if _, err := tx.ExecContext(ctx, sqlUnbindAllAttachments, uid, ticketID); err != nil {
			return fmt.Errorf("sync: unbind attachments: %w", err)
		}
		return nil
	}

	unbindArgs := []any{uid, ticketID}
	for _, id := range list {
		unbindArgs = append(unbindArgs, id)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(sqlUnbindOthersNotIn, placeholders(len(list))), unbindArgs...); err != nil {
		return fmt.Errorf("sync: unbind stale attachments: %w", err)
	}

	bindArgs := []any{ticketID, uid}
	for _, id := range list {
		bindArgs = append(bindArgs, id)
	}
	bindArgs = append(bindArgs, ticketID)
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(sqlBindAttachmentsIn, placeholders(len(list))), bindArgs...); err != nil {
		return fmt.Errorf("sync: bind attachments: %w", err)
	}
	return nil
}

// ── pull ────────────────────────────────────────────────────────────────────

type pullFilter struct {
	limit     int
	hasCursor bool
	curTime   time.Time
	curID     string
	rawCursor string // 无新数据时原样回显，客户端水位不回退
}

// txRow transactions LEFT JOIN tickets 的一行（含墓碑）。
type txRow struct {
	ID            string
	AmountCents   int64
	Direction     string
	CategoryID    int64
	Note          string
	OccurredAt    time.Time
	PaymentMethod string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     sql.NullTime
	TicketID      sql.NullString
}

// ticketRow tickets JOIN transactions 的一行（含墓碑）。
type ticketRow struct {
	ID            string
	TransactionID string
	Kind          string
	Title         string
	Venue         string
	EventTime     time.Time
	Seat          string
	ExtraJSON     []byte
	Rating        int
	Memo          sql.NullString
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     sql.NullTime
	AmountCents   int64
	CategoryID    int64
	PaymentMethod string
}

type attachmentRow struct {
	id                  int64
	ticketID            sql.NullString
	filePath, thumbPath string
	w, h                int
}

// pull 按 (updated_at, id) 全序增量下发 transactions + tickets（含墓碑），外加分类快照。
//
// 两表各取 limit+1 行（keyset），按同一全序归并后截断到 limit：被截掉的行严格晚于
// nextCursor，下一页必然重新取到，无重无漏。
func (s *service) pull(ctx context.Context, uid int64, f pullFilter) (*pullResult, error) {
	txRows, err := s.queryTxRows(ctx, uid, f)
	if err != nil {
		return nil, err
	}
	tkRows, err := s.queryTicketRows(ctx, uid, f)
	if err != nil {
		return nil, err
	}

	txRows, tkRows, hasMore, next := mergeTruncate(txRows, tkRows, f.limit)

	txs := make([]*Transaction, 0, len(txRows))
	for _, r := range txRows {
		txs = append(txs, renderTx(r))
	}

	attsByTicket, err := s.loadAttachments(ctx, uid, tkRows)
	if err != nil {
		return nil, err
	}
	tks := make([]*Ticket, 0, len(tkRows))
	for _, r := range tkRows {
		extra, err := parseExtraJSON(r.ExtraJSON)
		if err != nil {
			return nil, err
		}
		tks = append(tks, s.renderTicket(r, extra, attsByTicket[r.ID]))
	}

	nextCursor := f.rawCursor
	if next != "" {
		nextCursor = next
	}

	// categories 无 updated_at（契约 §9）→ 不能 keyset 增量。折中：仅在「初次全量」（无 since）
	// 或「本轮已追平」（hasMore=false，增量客户端的常态）时下发完整分类快照（含墓碑）；
	// 长回填的中间页省略，避免每页重复几十行。
	var cats []*Category
	if !f.hasCursor || !hasMore {
		if cats, err = s.queryCategories(ctx, uid); err != nil {
			return nil, err
		}
	} else {
		cats = []*Category{}
	}

	return &pullResult{
		Transactions: txs,
		Tickets:      tks,
		Categories:   cats,
		NextCursor:   nextCursor,
		HasMore:      hasMore,
	}, nil
}

// mergeTruncate 把两表结果按 (updated_at, id) 全序归并、截断到 limit，
// 返回截断后的两表分片、是否还有下一页、新游标（无行时为空串）。
func mergeTruncate(txs []txRow, tks []ticketRow, limit int) ([]txRow, []ticketRow, bool, string) {
	type key struct {
		at  time.Time
		id  string
		tkt bool
	}
	keys := make([]key, 0, len(txs)+len(tks))
	for _, r := range txs {
		keys = append(keys, key{at: r.UpdatedAt, id: r.ID})
	}
	for _, r := range tks {
		keys = append(keys, key{at: r.UpdatedAt, id: r.ID, tkt: true})
	}
	sort.SliceStable(keys, func(i, j int) bool {
		return beforeKey(keys[i].at, keys[i].id, keys[j].at, keys[j].id)
	})

	hasMore := len(keys) > limit
	if hasMore {
		keys = keys[:limit]
	}
	if len(keys) == 0 {
		return nil, nil, false, ""
	}

	nTx, nTk := 0, 0
	for _, k := range keys {
		if k.tkt {
			nTk++
		} else {
			nTx++
		}
	}
	last := keys[len(keys)-1]
	return txs[:nTx], tks[:nTk], hasMore, encodeCursor(last.at, last.id)
}

func (s *service) queryTxRows(ctx context.Context, uid int64, f pullFilter) ([]txRow, error) {
	q, args := sqlPullTxAll, []any{uid, f.limit + 1}
	if f.hasCursor {
		q, args = sqlPullTxSince, []any{uid, f.curTime, f.curTime, f.curID, f.limit + 1}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sync: pull transactions: %w", err)
	}
	defer rows.Close()

	var out []txRow
	for rows.Next() {
		var r txRow
		if err := rows.Scan(&r.ID, &r.AmountCents, &r.Direction, &r.CategoryID, &r.Note,
			&r.OccurredAt, &r.PaymentMethod, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt, &r.TicketID); err != nil {
			return nil, fmt.Errorf("sync: scan transaction: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sync: pull transactions rows: %w", err)
	}
	return out, nil
}

func (s *service) queryTicketRows(ctx context.Context, uid int64, f pullFilter) ([]ticketRow, error) {
	q, args := sqlPullTicketsAll, []any{uid, f.limit + 1}
	if f.hasCursor {
		q, args = sqlPullTicketsSince, []any{uid, f.curTime, f.curTime, f.curID, f.limit + 1}
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("sync: pull tickets: %w", err)
	}
	defer rows.Close()

	var out []ticketRow
	for rows.Next() {
		var r ticketRow
		if err := rows.Scan(&r.ID, &r.TransactionID, &r.Kind, &r.Title, &r.Venue, &r.EventTime, &r.Seat,
			&r.ExtraJSON, &r.Rating, &r.Memo, &r.CreatedAt, &r.UpdatedAt, &r.DeletedAt,
			&r.AmountCents, &r.CategoryID, &r.PaymentMethod); err != nil {
			return nil, fmt.Errorf("sync: scan ticket: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sync: pull tickets rows: %w", err)
	}
	return out, nil
}

// loadAttachments 批量拉本页票据的附件（避免 N+1）。
func (s *service) loadAttachments(ctx context.Context, uid int64, tks []ticketRow) (map[string][]attachmentRow, error) {
	out := make(map[string][]attachmentRow, len(tks))
	if len(tks) == 0 {
		return out, nil
	}
	args := []any{uid}
	for _, r := range tks {
		args = append(args, r.ID)
	}
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(sqlListAttachmentsIn, placeholders(len(tks))), args...)
	if err != nil {
		return nil, fmt.Errorf("sync: pull attachments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a attachmentRow
		if err := rows.Scan(&a.id, &a.ticketID, &a.filePath, &a.thumbPath, &a.w, &a.h); err != nil {
			return nil, fmt.Errorf("sync: scan attachment: %w", err)
		}
		if a.ticketID.Valid {
			out[a.ticketID.String] = append(out[a.ticketID.String], a)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sync: pull attachments rows: %w", err)
	}
	return out, nil
}

func (s *service) queryCategories(ctx context.Context, uid int64) ([]*Category, error) {
	rows, err := s.db.QueryContext(ctx, sqlPullCategories, uid)
	if err != nil {
		return nil, fmt.Errorf("sync: pull categories: %w", err)
	}
	defer rows.Close()

	out := []*Category{}
	for rows.Next() {
		var (
			c         Category
			userID    sql.NullInt64
			deletedAt sql.NullTime
		)
		if err := rows.Scan(&c.ID, &userID, &c.Name, &c.Icon, &c.Kind, &c.Sort, &deletedAt); err != nil {
			return nil, fmt.Errorf("sync: scan category: %w", err)
		}
		c.IsSystem = !userID.Valid
		c.DeletedAt = rfc3339Ptr(deletedAt.Time, deletedAt.Valid)
		out = append(out, &c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("sync: pull categories rows: %w", err)
	}
	return out, nil
}

// ── 渲染 ────────────────────────────────────────────────────────────────────

func renderTx(r txRow) *Transaction {
	var ticketID *string
	if r.TicketID.Valid {
		t := r.TicketID.String
		ticketID = &t
	}
	return &Transaction{
		ID: r.ID, AmountCents: r.AmountCents, Direction: r.Direction,
		CategoryID: r.CategoryID, Note: r.Note,
		OccurredAt: rfc3339(r.OccurredAt), PaymentMethod: r.PaymentMethod,
		TicketID:  ticketID,
		CreatedAt: rfc3339(r.CreatedAt), UpdatedAt: rfc3339(r.UpdatedAt),
		DeletedAt: rfc3339Ptr(r.DeletedAt.Time, r.DeletedAt.Valid),
	}
}

func (s *service) renderTicket(r ticketRow, extra map[string]string, atts []attachmentRow) *Ticket {
	out := make([]upload.Attachment, 0, len(atts))
	for _, a := range atts {
		out = append(out, upload.Attachment{
			ID:       a.id,
			URL:      upload.SignURL(s.secret, "/uploads/"+a.filePath, upload.URLTTL),
			ThumbURL: upload.SignURL(s.secret, "/uploads/"+a.thumbPath, upload.URLTTL),
			W:        a.w,
			H:        a.h,
		})
	}
	return &Ticket{
		ID: r.ID, Kind: r.Kind, Title: r.Title, Venue: r.Venue,
		EventTime: rfc3339(r.EventTime), Seat: r.Seat,
		Extra: fillExtraDefaults(r.Kind, extra), Rating: r.Rating, Memo: r.Memo.String,
		Transaction: TxSummary{
			ID: r.TransactionID, AmountCents: r.AmountCents,
			CategoryID: r.CategoryID, PaymentMethod: r.PaymentMethod,
		},
		Attachments: out,
		CreatedAt:   rfc3339(r.CreatedAt), UpdatedAt: rfc3339(r.UpdatedAt),
		DeletedAt: rfc3339Ptr(r.DeletedAt.Time, r.DeletedAt.Valid),
	}
}

func parseExtraJSON(raw []byte) (map[string]string, error) {
	m := map[string]string{}
	if len(raw) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("sync: corrupt extra json: %w", err)
	}
	return m, nil
}

// ── 杂项 ────────────────────────────────────────────────────────────────────

func commit(tx *sql.Tx, op string) error {
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return nil
}

func mapInsertErr(err error, op string) error {
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == mysqlErrDuplicateEntry {
		return apperr.New(apperr.CodeConflict, "id already exists")
	}
	return fmt.Errorf("%s: %w", op, err)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
