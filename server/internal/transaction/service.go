package transaction

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/apperr"
)

// SQL 常量（测试用 regexp.QuoteMeta 复用，防实现/断言漂移）。
// 所有语句一律带 user_id 条件（conventions §2：漏带即安全 bug）。
//
// 契约 §4 要求 Transaction.ticketId 为从 tickets 表派生的只读反向关联，
// 因此这里对 tickets 做只读 LEFT JOIN——契约对「模块不互查对方表」禁令的显式豁免；
// 除本文件的读 JOIN 外，不得写 tickets（票↔交易联动写由 ticket 模块负责）。
const (
	sqlSelectTx = "SELECT tx.id, tx.amount_cents, tx.direction, tx.category_id, tx.note, tx.occurred_at, tx.payment_method, tx.created_at, tx.updated_at, tk.id" +
		" FROM transactions tx LEFT JOIN tickets tk ON tk.transaction_id = tx.id AND tk.user_id = tx.user_id AND tk.deleted_at IS NULL"

	sqlGetTxForUpdate = sqlSelectTx + " WHERE tx.id = ? AND tx.user_id = ? AND tx.deleted_at IS NULL FOR UPDATE"

	// POST 幂等检查：软删行也算「存在」（LWW 复活），故不过滤 tx.deleted_at。
	sqlSelectTxMeta = "SELECT tx.direction, tx.created_at, tx.updated_at, tk.id" +
		" FROM transactions tx LEFT JOIN tickets tk ON tk.transaction_id = tx.id AND tk.user_id = tx.user_id AND tk.deleted_at IS NULL" +
		" WHERE tx.id = ? AND tx.user_id = ? FOR UPDATE"

	sqlInsertTx = "INSERT INTO transactions (id, user_id, amount_cents, direction, category_id, note, occurred_at, payment_method, created_at, updated_at)" +
		" VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"

	// POST 同 id 重放（LWW upsert）：整体覆盖并清墓碑。
	// direction 建后不可改（契约 v1.1），不在 SET 内；重放改向在 service 内拒 40001。
	sqlUpsertTx = "UPDATE transactions SET amount_cents = ?, category_id = ?, note = ?, occurred_at = ?, payment_method = ?, updated_at = ?, deleted_at = NULL WHERE id = ? AND user_id = ?"

	// DELETE 软删，bump updated_at 供 sync 墓碑下发。
	sqlSoftDeleteTx = "UPDATE transactions SET updated_at = ?, deleted_at = ? WHERE id = ? AND user_id = ?"

	mysqlErrDuplicateEntry = 1062
)

type service struct {
	db  *sql.DB
	now func() time.Time // 测试注入固定时钟
}

// txRow transactions LEFT JOIN tickets 的一行。
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
	TicketID      sql.NullString
}

func (r *txRow) scan(row interface{ Scan(...any) error }) error {
	return row.Scan(&r.ID, &r.AmountCents, &r.Direction, &r.CategoryID, &r.Note,
		&r.OccurredAt, &r.PaymentMethod, &r.CreatedAt, &r.UpdatedAt, &r.TicketID)
}

// render 渲染契约 §4 Transaction。
func render(r txRow) *Transaction {
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
	}
}

func errNotFound() error {
	return apperr.New(apperr.CodeNotFound, "transaction not found")
}

// ── List ────────────────────────────────────────────────────────────────────

type listFilter struct {
	hasMonth   bool
	monthStart time.Time // UTC 月初（含）
	monthEnd   time.Time // UTC 下月初（不含）
	categoryID int64     // 0 = 不过滤
	direction  string    // "" = 不过滤
	limit      int
	hasCursor  bool
	curTime    time.Time
	curID      string
}

type listResult struct {
	Items      []*Transaction `json:"items"`
	NextCursor *string        `json:"nextCursor"`
}

// list occurred_at DESC, id DESC keyset 分页；多取 1 行判断是否还有下一页。
func (s *service) list(ctx context.Context, uid int64, f listFilter) (*listResult, error) {
	q := sqlSelectTx + " WHERE tx.user_id = ? AND tx.deleted_at IS NULL"
	args := []any{uid}
	if f.hasMonth {
		q += " AND tx.occurred_at >= ? AND tx.occurred_at < ?"
		args = append(args, f.monthStart, f.monthEnd)
	}
	if f.categoryID != 0 {
		q += " AND tx.category_id = ?"
		args = append(args, f.categoryID)
	}
	if f.direction != "" {
		q += " AND tx.direction = ?"
		args = append(args, f.direction)
	}
	if f.hasCursor {
		q += " AND (tx.occurred_at < ? OR (tx.occurred_at = ? AND tx.id < ?))"
		args = append(args, f.curTime, f.curTime, f.curID)
	}
	q += " ORDER BY tx.occurred_at DESC, tx.id DESC LIMIT ?"
	args = append(args, f.limit+1)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("transaction: list query: %w", err)
	}
	defer rows.Close()

	var trs []txRow
	for rows.Next() {
		var r txRow
		if err := r.scan(rows); err != nil {
			return nil, fmt.Errorf("transaction: list scan: %w", err)
		}
		trs = append(trs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("transaction: list rows: %w", err)
	}

	var nextCursor *string
	if len(trs) > f.limit {
		trs = trs[:f.limit]
		last := trs[len(trs)-1]
		c := encodeCursor(last.OccurredAt, last.ID)
		nextCursor = &c
	}

	items := make([]*Transaction, 0, len(trs))
	for _, r := range trs {
		items = append(items, render(r))
	}
	return &listResult{Items: items, NextCursor: nextCursor}, nil
}

// ── Create（POST，幂等 upsert）───────────────────────────────────────────────

// create DB 事务内幂等 upsert：
//   - 全新 id → INSERT；主键被他人占用（或并发重放）→ 主键冲突 → 40902，不泄漏归属；
//   - 同 id 已存在 → LWW 覆盖（含复活软删行）；携带 updatedAt 且比服务端旧 → 40902；
//   - 重放不允许改 direction（与 PATCH 同规则）→ 40001。
func (s *service) create(ctx context.Context, uid int64, d createData) (*Transaction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("transaction: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // commit 后为 ErrTxDone no-op

	now := s.now().UTC().Truncate(time.Millisecond)

	var (
		direction string
		createdAt time.Time
		updatedAt time.Time
		ticketID  sql.NullString
	)
	err = tx.QueryRowContext(ctx, sqlSelectTxMeta, d.ID, uid).Scan(&direction, &createdAt, &updatedAt, &ticketID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, sqlInsertTx,
			d.ID, uid, d.AmountCents, d.Direction, d.CategoryID, d.Note, d.OccurredAt, d.PaymentMethod, now, now); err != nil {
			return nil, mapInsertErr(err, "transaction: insert")
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("transaction: commit: %w", err)
		}
		return render(rowFromCreate(d, now, now, sql.NullString{})), nil
	case err != nil:
		return nil, fmt.Errorf("transaction: idempotency check: %w", err)
	}

	// 幂等重放：LWW。客户端时间戳更旧 → 40902（契约 §1）。
	if d.ClientUpdated != nil && d.ClientUpdated.Before(updatedAt) {
		return nil, apperr.New(apperr.CodeConflict, "stale write: server has newer version")
	}
	if d.Direction != direction {
		return nil, badParam("direction cannot be changed")
	}
	if _, err := tx.ExecContext(ctx, sqlUpsertTx,
		d.AmountCents, d.CategoryID, d.Note, d.OccurredAt, d.PaymentMethod, now, d.ID, uid); err != nil {
		return nil, fmt.Errorf("transaction: upsert: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("transaction: commit: %w", err)
	}
	return render(rowFromCreate(d, createdAt, now, ticketID)), nil
}

func rowFromCreate(d createData, createdAt, updatedAt time.Time, ticketID sql.NullString) txRow {
	return txRow{
		ID: d.ID, AmountCents: d.AmountCents, Direction: d.Direction,
		CategoryID: d.CategoryID, Note: d.Note,
		OccurredAt: d.OccurredAt, PaymentMethod: d.PaymentMethod,
		CreatedAt: createdAt, UpdatedAt: updatedAt, TicketID: ticketID,
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

// patch 部分更新；direction 与现值不同 → 40001（契约 v1.1：PATCH 不允许改 direction）。
func (s *service) patch(ctx context.Context, uid int64, id string, p patchData) (*Transaction, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("transaction: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var r txRow
	err = r.scan(tx.QueryRowContext(ctx, sqlGetTxForUpdate, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound() // 不存在 / 已删 / 他人的交易，一律 40401 不泄漏
	}
	if err != nil {
		return nil, fmt.Errorf("transaction: patch load: %w", err)
	}

	if p.Direction != nil && *p.Direction != r.Direction {
		return nil, badParam("direction cannot be changed")
	}

	// 动态 SET（固定字段顺序，保证语句可测）。
	var sets []string
	var args []any
	addSet := func(col string, v any) { sets = append(sets, col+" = ?"); args = append(args, v) }
	if p.AmountCents != nil {
		addSet("amount_cents", *p.AmountCents)
		r.AmountCents = *p.AmountCents
	}
	if p.CategoryID != nil {
		addSet("category_id", *p.CategoryID)
		r.CategoryID = *p.CategoryID
	}
	if p.Note != nil {
		addSet("note", *p.Note)
		r.Note = *p.Note
	}
	if p.OccurredAt != nil {
		addSet("occurred_at", *p.OccurredAt)
		r.OccurredAt = *p.OccurredAt
	}
	if p.PaymentMethod != nil {
		addSet("payment_method", *p.PaymentMethod)
		r.PaymentMethod = *p.PaymentMethod
	}

	if len(sets) > 0 {
		now := s.now().UTC().Truncate(time.Millisecond)
		r.UpdatedAt = now
		q := "UPDATE transactions SET " + strings.Join(append(sets, "updated_at = ?"), ", ") + " WHERE id = ? AND user_id = ?"
		if _, err := tx.ExecContext(ctx, q, append(args, now, id, uid)...); err != nil {
			return nil, fmt.Errorf("transaction: patch update: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("transaction: commit: %w", err)
	}
	return render(r), nil
}

// ── Delete ──────────────────────────────────────────────────────────────────

// remove 软删；票据关联的交易必须从票夹删除（契约 v1.1：ticketId 非空 → 40001）。
func (s *service) remove(ctx context.Context, uid int64, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("transaction: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var r txRow
	err = r.scan(tx.QueryRowContext(ctx, sqlGetTxForUpdate, id, uid))
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound()
	}
	if err != nil {
		return fmt.Errorf("transaction: delete load: %w", err)
	}
	if r.TicketID.Valid {
		return badParam("请从票夹删除该票据")
	}

	now := s.now().UTC().Truncate(time.Millisecond)
	if _, err := tx.ExecContext(ctx, sqlSoftDeleteTx, now, now, id, uid); err != nil {
		return fmt.Errorf("transaction: soft delete: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("transaction: commit: %w", err)
	}
	return nil
}
