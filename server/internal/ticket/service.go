package ticket

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/apperr"
	"piaoju/internal/upload"
)

// SQL 常量（测试用 regexp.QuoteMeta 复用，防实现/断言漂移）。
// 所有语句一律带 user_id 条件（conventions §2：漏带即安全 bug）。
//
// 契约 §5 要求 Ticket 内嵌 transaction{id,amountCents,categoryId,paymentMethod}，
// 因此这里对 transactions 做只读 JOIN——契约对「模块不互查对方表」禁令的显式豁免；
// 除本文件的读 JOIN 与同事务联动写外，不得扩大对 transactions 的访问面。
const (
	sqlSelectTicketJoin = "SELECT t.id, t.transaction_id, t.kind, t.title, t.venue, t.event_time, t.seat, t.extra, t.rating, t.memo, t.created_at, t.updated_at, tx.amount_cents, tx.category_id, tx.payment_method" +
		" FROM tickets t JOIN transactions tx ON tx.id = t.transaction_id"

	sqlGetTicket          = sqlSelectTicketJoin + " WHERE t.id = ? AND t.user_id = ? AND t.deleted_at IS NULL"
	sqlGetTicketForUpdate = sqlGetTicket + " FOR UPDATE"

	// POST 幂等检查：软删行也算「存在」（LWW 复活），故不过滤 deleted_at。
	sqlSelectTicketMeta = "SELECT transaction_id, created_at, updated_at FROM tickets WHERE id = ? AND user_id = ? FOR UPDATE"

	sqlInsertTransaction = "INSERT INTO transactions (id, user_id, amount_cents, direction, category_id, note, occurred_at, payment_method, created_at, updated_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	sqlInsertTicket = "INSERT INTO tickets (id, user_id, transaction_id, kind, title, venue, event_time, seat, extra, rating, memo, created_at, updated_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	// POST 同 id 重放（LWW upsert）：整体覆盖并清墓碑。
	sqlUpsertTransaction = "UPDATE transactions SET amount_cents = ?, category_id = ?, payment_method = ?, note = ?, occurred_at = ?, updated_at = ?, deleted_at = NULL WHERE id = ? AND user_id = ?"
	sqlUpsertTicket      = "UPDATE tickets SET kind = ?, title = ?, venue = ?, event_time = ?, seat = ?, extra = ?, rating = ?, memo = ?, updated_at = ?, deleted_at = NULL WHERE id = ? AND user_id = ?"

	// DELETE 同事务双软删（票 + 关联交易），bump updated_at 供 sync 墓碑下发。
	sqlSoftDeleteTicket      = "UPDATE tickets SET updated_at = ?, deleted_at = ? WHERE id = ? AND user_id = ?"
	sqlSoftDeleteTransaction = "UPDATE transactions SET updated_at = ?, deleted_at = ? WHERE id = ? AND user_id = ?"

	sqlSelectAttachmentsByTicket = "SELECT id, file_path, thumb_path, w, h FROM attachments WHERE user_id = ? AND ticket_id = ? ORDER BY id"
	sqlUnbindAllAttachments      = "UPDATE attachments SET ticket_id = NULL WHERE user_id = ? AND ticket_id = ?"
)

// 动态 IN(...) 语句模板（%s 为占位符串），测试用同款 helper 构造。
const (
	sqlLockAttachmentsIn   = "SELECT id, ticket_id, file_path, thumb_path, w, h FROM attachments WHERE user_id = ? AND id IN (%s) FOR UPDATE"
	sqlBindAttachmentsIn   = "UPDATE attachments SET ticket_id = ? WHERE user_id = ? AND id IN (%s)"
	sqlUnbindOthersNotIn   = "UPDATE attachments SET ticket_id = NULL WHERE user_id = ? AND ticket_id = ? AND id NOT IN (%s)"
	sqlListAttachmentsIn   = "SELECT id, ticket_id, file_path, thumb_path, w, h FROM attachments WHERE user_id = ? AND ticket_id IN (%s) ORDER BY ticket_id, id"
	mysqlErrDuplicateEntry = 1062
)

type service struct {
	db     *sql.DB
	secret string           // upload 签名密钥（生成附件 url/thumbUrl）
	now    func() time.Time // 测试注入固定时钟
}

// ticketRow tickets JOIN transactions 的一行。
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
	AmountCents   int64
	CategoryID    int64
	PaymentMethod string
}

func (r *ticketRow) scan(row interface{ Scan(...any) error }) error {
	return row.Scan(&r.ID, &r.TransactionID, &r.Kind, &r.Title, &r.Venue, &r.EventTime, &r.Seat,
		&r.ExtraJSON, &r.Rating, &r.Memo, &r.CreatedAt, &r.UpdatedAt,
		&r.AmountCents, &r.CategoryID, &r.PaymentMethod)
}

// attachmentRow attachments 表行（含内部路径，渲染时转签名 URL）。
type attachmentRow struct {
	id                  int64
	ticketID            sql.NullString
	filePath, thumbPath string
	w, h                int
}

// render 渲染契约 §5 Ticket；附件 url/thumbUrl 用 upload.SignURL 生成（7 天有效期）。
func (s *service) render(r ticketRow, extra map[string]string, atts []attachmentRow) *Ticket {
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
	}
}

func parseExtraJSON(raw []byte) (map[string]string, error) {
	m := map[string]string{}
	if len(raw) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("ticket: corrupt extra json: %w", err)
	}
	return m, nil
}

// ── List ────────────────────────────────────────────────────────────────────

type listFilter struct {
	kind      string // "" = 不过滤
	year      int    // 0 = 不过滤
	limit     int
	hasCursor bool
	curTime   time.Time
	curID     string
}

type listResult struct {
	Items      []*Ticket `json:"items"`
	NextCursor *string   `json:"nextCursor"`
}

// list event_time DESC, id DESC keyset 分页；多取 1 行判断是否还有下一页。
func (s *service) list(ctx context.Context, uid int64, f listFilter) (*listResult, error) {
	q := sqlSelectTicketJoin + " WHERE t.user_id = ? AND t.deleted_at IS NULL"
	args := []any{uid}
	if f.kind != "" {
		q += " AND t.kind = ?"
		args = append(args, f.kind)
	}
	if f.year != 0 {
		start := time.Date(f.year, 1, 1, 0, 0, 0, 0, time.UTC)
		q += " AND t.event_time >= ? AND t.event_time < ?"
		args = append(args, start, start.AddDate(1, 0, 0))
	}
	if f.hasCursor {
		q += " AND (t.event_time < ? OR (t.event_time = ? AND t.id < ?))"
		args = append(args, f.curTime, f.curTime, f.curID)
	}
	q += " ORDER BY t.event_time DESC, t.id DESC LIMIT ?"
	args = append(args, f.limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ticket: list query: %w", err)
	}
	defer rows.Close()

	var trs []ticketRow
	for rows.Next() {
		var r ticketRow
		if err := r.scan(rows); err != nil {
			return nil, fmt.Errorf("ticket: list scan: %w", err)
		}
		trs = append(trs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ticket: list rows: %w", err)
	}

	var nextCursor *string
	if len(trs) > f.limit {
		trs = trs[:f.limit]
		last := trs[len(trs)-1]
		c := encodeCursor(last.EventTime, last.ID)
		nextCursor = &c
	}

	attsByTicket, err := s.loadAttachmentsForTickets(ctx, uid, trs)
	if err != nil {
		return nil, err
	}

	items := make([]*Ticket, 0, len(trs))
	for _, r := range trs {
		extra, err := parseExtraJSON(r.ExtraJSON)
		if err != nil {
			return nil, err
		}
		items = append(items, s.render(r, extra, attsByTicket[r.ID]))
	}
	return &listResult{Items: items, NextCursor: nextCursor}, nil
}

// loadAttachmentsForTickets 批量拉当前页所有票的附件（避免 N+1）。
func (s *service) loadAttachmentsForTickets(ctx context.Context, uid int64, trs []ticketRow) (map[string][]attachmentRow, error) {
	out := make(map[string][]attachmentRow, len(trs))
	if len(trs) == 0 {
		return out, nil
	}
	args := []any{uid}
	for _, r := range trs {
		args = append(args, r.ID)
	}
	q := fmt.Sprintf(sqlListAttachmentsIn, placeholders(len(trs)))
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ticket: list attachments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a attachmentRow
		if err := rows.Scan(&a.id, &a.ticketID, &a.filePath, &a.thumbPath, &a.w, &a.h); err != nil {
			return nil, fmt.Errorf("ticket: attachments scan: %w", err)
		}
		if a.ticketID.Valid {
			out[a.ticketID.String] = append(out[a.ticketID.String], a)
		}
	}
	return out, rows.Err()
}

// ── Get ─────────────────────────────────────────────────────────────────────

func (s *service) get(ctx context.Context, uid int64, id string) (*Ticket, error) {
	var r ticketRow
	err := r.scan(s.db.QueryRowContext(ctx, sqlGetTicket, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound()
	}
	if err != nil {
		return nil, fmt.Errorf("ticket: get: %w", err)
	}
	extra, err := parseExtraJSON(r.ExtraJSON)
	if err != nil {
		return nil, err
	}
	atts, err := s.queryAttachmentsByTicket(ctx, s.db, uid, id)
	if err != nil {
		return nil, err
	}
	return s.render(r, extra, atts), nil
}

func errNotFound() error {
	return apperr.New(apperr.CodeNotFound, "ticket not found")
}

// ── Create（POST，幂等 upsert）───────────────────────────────────────────────

// create DB 事务内：insert transactions（id 服务端 UUID、direction 恒 expense）→ insert tickets → 绑附件。
// 同 id 已存在 → LWW 覆盖（含复活软删行）；携带 updatedAt 且比服务端旧 → 40902。
func (s *service) create(ctx context.Context, uid int64, d createData) (*Ticket, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("ticket: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // commit 后为 ErrTxDone no-op

	now := s.now().UTC().Truncate(time.Millisecond)
	extraJSON, err := json.Marshal(d.Extra)
	if err != nil {
		return nil, fmt.Errorf("ticket: marshal extra: %w", err)
	}

	var (
		existingTxID string
		createdAt    time.Time
		updatedAt    time.Time
	)
	err = tx.QueryRowContext(ctx, sqlSelectTicketMeta, d.ID, uid).Scan(&existingTxID, &createdAt, &updatedAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return s.createInsert(ctx, tx, uid, d, extraJSON, now)
	case err != nil:
		return nil, fmt.Errorf("ticket: idempotency check: %w", err)
	}

	// 幂等重放：LWW。客户端时间戳更旧 → 40902（契约 §1）。
	if d.ClientUpdated != nil && d.ClientUpdated.Before(updatedAt) {
		return nil, apperr.New(apperr.CodeConflict, "stale write: server has newer version")
	}

	atts, err := s.lockAttachments(ctx, tx, uid, d.AttachmentIDs, d.ID)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, sqlUpsertTransaction,
		d.AmountCents, d.CategoryID, d.PaymentMethod, d.Title, d.OccurredAt, now, existingTxID, uid); err != nil {
		return nil, fmt.Errorf("ticket: upsert transaction: %w", err)
	}
	if _, err := tx.ExecContext(ctx, sqlUpsertTicket,
		d.Kind, d.Title, d.Venue, d.EventTime, d.Seat, extraJSON, d.Rating, d.Memo, now, d.ID, uid); err != nil {
		return nil, fmt.Errorf("ticket: upsert ticket: %w", err)
	}
	if err := s.rebindAttachments(ctx, tx, uid, d.ID, d.AttachmentIDs); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("ticket: commit: %w", err)
	}
	return s.render(rowFromCreate(d, existingTxID, createdAt, now), d.Extra, atts), nil
}

// createInsert 全新 id 的插入路径。事务顺序：insert transactions → insert tickets →（绑附件）→ commit。
func (s *service) createInsert(ctx context.Context, tx *sql.Tx, uid int64, d createData, extraJSON []byte, now time.Time) (*Ticket, error) {
	atts, err := s.lockAttachments(ctx, tx, uid, d.AttachmentIDs, d.ID)
	if err != nil {
		return nil, err
	}
	// 交易主键由客户端生成（契约 §5 v1.2）——离线建票时本地须立刻把这笔交易写进账本，
	// 等服务端生成就意味着离线期账本缺这一笔。note = title 快照。
	txID := d.TransactionID
	if _, err := tx.ExecContext(ctx, sqlInsertTransaction,
		txID, uid, d.AmountCents, "expense", d.CategoryID, d.Title, d.OccurredAt, d.PaymentMethod, now, now); err != nil {
		return nil, mapInsertErr(err, "ticket: insert transaction")
	}
	if _, err := tx.ExecContext(ctx, sqlInsertTicket,
		d.ID, uid, txID, d.Kind, d.Title, d.Venue, d.EventTime, d.Seat, extraJSON, d.Rating, d.Memo, now, now); err != nil {
		// 同 id 被其他用户占用（或并发重放）→ 主键冲突 → 40902，不泄漏归属。
		return nil, mapInsertErr(err, "ticket: insert ticket")
	}
	if len(d.AttachmentIDs) > 0 {
		if err := s.bindAttachments(ctx, tx, uid, d.ID, d.AttachmentIDs); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("ticket: commit: %w", err)
	}
	return s.render(rowFromCreate(d, txID, now, now), d.Extra, atts), nil
}

func rowFromCreate(d createData, txID string, createdAt, updatedAt time.Time) ticketRow {
	return ticketRow{
		ID: d.ID, TransactionID: txID, Kind: d.Kind, Title: d.Title, Venue: d.Venue,
		EventTime: d.EventTime, Seat: d.Seat, Rating: d.Rating,
		Memo:      sql.NullString{String: d.Memo, Valid: true},
		CreatedAt: createdAt, UpdatedAt: updatedAt,
		AmountCents: d.AmountCents, CategoryID: d.CategoryID, PaymentMethod: d.PaymentMethod,
	}
}

func mapInsertErr(err error, op string) error {
	var me *mysql.MySQLError
	if errors.As(err, &me) && me.Number == mysqlErrDuplicateEntry {
		return apperr.New(apperr.CodeConflict, "id already exists")
	}
	return fmt.Errorf("%s: %w", op, err)
}

// ── Patch ───────────────────────────────────────────────────────────────────

// patch 部分更新；amountCents/categoryId/paymentMethod/occurredAt 变更同事务同步改关联交易（契约 §5）。
func (s *service) patch(ctx context.Context, uid int64, id string, p patchData) (*Ticket, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("ticket: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var r ticketRow
	err = r.scan(tx.QueryRowContext(ctx, sqlGetTicketForUpdate, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound() // 不存在 / 已删 / 他人的票，一律 40401 不泄漏
	}
	if err != nil {
		return nil, fmt.Errorf("ticket: patch load: %w", err)
	}

	extra, err := parseExtraJSON(r.ExtraJSON)
	if err != nil {
		return nil, err
	}

	// 生效 kind + extra：改 kind 必须重校验 extra 白名单。
	kind := r.Kind
	if p.Kind != nil {
		kind = *p.Kind
	}
	extraChanged := false
	switch {
	case p.Extra != nil:
		if extra, err = normalizeExtra(kind, p.Extra); err != nil {
			return nil, err
		}
		extraChanged = true
	case p.Kind != nil && *p.Kind != r.Kind:
		if extra, err = reconcileExtra(kind, extra); err != nil {
			return nil, err
		}
		extraChanged = true
	}

	// tickets 动态 SET（固定字段顺序，保证语句可测）。
	var sets []string
	var args []any
	addSet := func(col string, v any) { sets = append(sets, col+" = ?"); args = append(args, v) }
	if p.Kind != nil {
		addSet("kind", *p.Kind)
		r.Kind = *p.Kind
	}
	if p.Title != nil {
		addSet("title", *p.Title)
		r.Title = *p.Title
	}
	if p.Venue != nil {
		addSet("venue", *p.Venue)
		r.Venue = *p.Venue
	}
	if p.EventTime != nil {
		addSet("event_time", *p.EventTime)
		r.EventTime = *p.EventTime
	}
	if p.Seat != nil {
		addSet("seat", *p.Seat)
		r.Seat = *p.Seat
	}
	if extraChanged {
		b, err := json.Marshal(extra)
		if err != nil {
			return nil, fmt.Errorf("ticket: marshal extra: %w", err)
		}
		addSet("extra", b)
	}
	if p.Rating != nil {
		addSet("rating", *p.Rating)
		r.Rating = *p.Rating
	}
	if p.Memo != nil {
		addSet("memo", *p.Memo)
		r.Memo = sql.NullString{String: *p.Memo, Valid: true}
	}

	// transactions 联动 SET。
	var txSets []string
	var txArgs []any
	addTxSet := func(col string, v any) { txSets = append(txSets, col+" = ?"); txArgs = append(txArgs, v) }
	if p.AmountCents != nil {
		addTxSet("amount_cents", *p.AmountCents)
		r.AmountCents = *p.AmountCents
	}
	if p.CategoryID != nil {
		addTxSet("category_id", *p.CategoryID)
		r.CategoryID = *p.CategoryID
	}
	if p.PaymentMethod != nil {
		addTxSet("payment_method", *p.PaymentMethod)
		r.PaymentMethod = *p.PaymentMethod
	}
	if p.OccurredAt != nil {
		addTxSet("occurred_at", *p.OccurredAt)
	}

	changed := len(sets) > 0 || len(txSets) > 0 || p.AttachmentIDs != nil
	var atts []attachmentRow
	if changed {
		now := s.now().UTC().Truncate(time.Millisecond)
		r.UpdatedAt = now
		// 任何有效变更（含仅交易字段/附件变更）都 bump tickets.updated_at，票据实体对 sync 可见。
		q := "UPDATE tickets SET " + strings.Join(append(sets, "updated_at = ?"), ", ") + " WHERE id = ? AND user_id = ?"
		if _, err := tx.ExecContext(ctx, q, append(args, now, id, uid)...); err != nil {
			return nil, fmt.Errorf("ticket: patch update ticket: %w", err)
		}
		if len(txSets) > 0 {
			q := "UPDATE transactions SET " + strings.Join(append(txSets, "updated_at = ?"), ", ") + " WHERE id = ? AND user_id = ?"
			if _, err := tx.ExecContext(ctx, q, append(txArgs, now, r.TransactionID, uid)...); err != nil {
				return nil, fmt.Errorf("ticket: patch update transaction: %w", err)
			}
		}
	}
	if p.AttachmentIDs != nil {
		if atts, err = s.lockAttachments(ctx, tx, uid, *p.AttachmentIDs, id); err != nil {
			return nil, err
		}
		if err := s.rebindAttachments(ctx, tx, uid, id, *p.AttachmentIDs); err != nil {
			return nil, err
		}
	} else {
		if atts, err = s.queryAttachmentsByTicket(ctx, tx, uid, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("ticket: commit: %w", err)
	}
	return s.render(r, extra, atts), nil
}

// ── Delete ──────────────────────────────────────────────────────────────────

// remove 同事务软删票 + 关联交易（契约 §5：DELETE 软删票 + 关联交易）。
func (s *service) remove(ctx context.Context, uid int64, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ticket: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var txID string
	err = tx.QueryRowContext(ctx,
		"SELECT transaction_id FROM tickets WHERE id = ? AND user_id = ? AND deleted_at IS NULL FOR UPDATE",
		id, uid).Scan(&txID)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound()
	}
	if err != nil {
		return fmt.Errorf("ticket: delete load: %w", err)
	}

	now := s.now().UTC().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, sqlSoftDeleteTicket, now, now, id, uid); err != nil {
		return fmt.Errorf("ticket: soft delete ticket: %w", err)
	}
	if _, err := tx.ExecContext(ctx, sqlSoftDeleteTransaction, now, now, txID, uid); err != nil {
		return fmt.Errorf("ticket: soft delete transaction: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ticket: commit: %w", err)
	}
	return nil
}

// ── attachments 绑定 ─────────────────────────────────────────────────────────

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryAttachmentsByTicket 拉某票当前绑定的附件（Get / 未改附件的 Patch 响应用）。
func (s *service) queryAttachmentsByTicket(ctx context.Context, q queryer, uid int64, ticketID string) ([]attachmentRow, error) {
	rows, err := q.QueryContext(ctx, sqlSelectAttachmentsByTicket, uid, ticketID)
	if err != nil {
		return nil, fmt.Errorf("ticket: query attachments: %w", err)
	}
	defer rows.Close()
	var out []attachmentRow
	for rows.Next() {
		var a attachmentRow
		if err := rows.Scan(&a.id, &a.filePath, &a.thumbPath, &a.w, &a.h); err != nil {
			return nil, fmt.Errorf("ticket: scan attachment: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// lockAttachments 校验 attachmentIds：必须属于本人且未绑到其他票（40001），行锁防并发抢绑。
// 返回按入参顺序排列的附件行（响应渲染复用，免二次查询）。
func (s *service) lockAttachments(ctx context.Context, tx *sql.Tx, uid int64, ids []int64, ticketID string) ([]attachmentRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	args := []any{uid}
	for _, id := range ids {
		args = append(args, id)
	}
	q := fmt.Sprintf(sqlLockAttachmentsIn, placeholders(len(ids)))
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ticket: lock attachments: %w", err)
	}
	defer rows.Close()

	byID := make(map[int64]attachmentRow, len(ids))
	for rows.Next() {
		var a attachmentRow
		if err := rows.Scan(&a.id, &a.ticketID, &a.filePath, &a.thumbPath, &a.w, &a.h); err != nil {
			return nil, fmt.Errorf("ticket: scan attachment: %w", err)
		}
		byID[a.id] = a
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ticket: lock attachments rows: %w", err)
	}

	out := make([]attachmentRow, 0, len(ids))
	for _, id := range ids {
		a, ok := byID[id]
		if !ok { // 不存在或不属于本人，同样 40001 不泄漏归属
			return nil, badParam("attachment %d not found", id)
		}
		if a.ticketID.Valid && a.ticketID.String != ticketID {
			return nil, badParam("attachment %d already attached to another ticket", id)
		}
		out = append(out, a)
	}
	return out, nil
}

// bindAttachments 绑定 = set ticket_id（任务卡 §5.7）。
func (s *service) bindAttachments(ctx context.Context, tx *sql.Tx, uid int64, ticketID string, ids []int64) error {
	args := []any{ticketID, uid}
	for _, id := range ids {
		args = append(args, id)
	}
	q := fmt.Sprintf(sqlBindAttachmentsIn, placeholders(len(ids)))
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("ticket: bind attachments: %w", err)
	}
	return nil
}

// rebindAttachments 全量集合语义：不在新列表里的旧绑定解绑，再绑定新列表。
func (s *service) rebindAttachments(ctx context.Context, tx *sql.Tx, uid int64, ticketID string, ids []int64) error {
	if len(ids) == 0 {
		if _, err := tx.ExecContext(ctx, sqlUnbindAllAttachments, uid, ticketID); err != nil {
			return fmt.Errorf("ticket: unbind attachments: %w", err)
		}
		return nil
	}
	args := []any{uid, ticketID}
	for _, id := range ids {
		args = append(args, id)
	}
	q := fmt.Sprintf(sqlUnbindOthersNotIn, placeholders(len(ids)))
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return fmt.Errorf("ticket: unbind stale attachments: %w", err)
	}
	return s.bindAttachments(ctx, tx, uid, ticketID, ids)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
