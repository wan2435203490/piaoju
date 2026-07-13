package transaction

// service 层单测：本机无 Docker/MySQL（同 platform/db 约束），用 go-sqlmock 精确
// 断言 SQL 语句 + 参数（regexp.QuoteMeta 复用 service.go 的 SQL 常量，防实现/断言漂移）。
// 真库执行路径由 Wave 3 集成阶段 docker compose 冒烟覆盖。
//
// user_id 隔离的断言方式：所有 WithArgs 均含 uid，且 SQL 常量本身带 user_id = ? 条件；
// 他人行为 ErrNoRows → 40401（见 TestPatchOtherUsersRow / TestDeleteNotFound）。

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/apperr"
)

const (
	uidA int64 = 7
	txID       = "c0fed131-7235-4d6b-814e-bf3786bdc01e"
)

var (
	fixedNow = time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	occurred = time.Date(2026, 7, 10, 3, 4, 5, 0, time.UTC)
	created  = time.Date(2026, 7, 10, 3, 5, 0, 0, time.UTC)
)

func newTestService(t *testing.T) (*service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &service{db: db, now: func() time.Time { return fixedNow }}, mock
}

func mustMeet(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

func wantCode(t *testing.T, err error, code int) {
	t.Helper()
	var ae *apperr.Error
	if !errors.As(err, &ae) || ae.Code != code {
		t.Fatalf("err = %v, want apperr code %d", err, code)
	}
}

// txRowsCols 与 sqlSelectTx 的列序一一对应。
func txRowsCols() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "amount_cents", "direction", "category_id", "note",
		"occurred_at", "payment_method", "created_at", "updated_at", "tk_id",
	})
}

func addRow(rows *sqlmock.Rows, r txRow) *sqlmock.Rows {
	var tk any
	if r.TicketID.Valid {
		tk = r.TicketID.String
	}
	return rows.AddRow(r.ID, r.AmountCents, r.Direction, r.CategoryID, r.Note,
		r.OccurredAt, r.PaymentMethod, r.CreatedAt, r.UpdatedAt, tk)
}

func sampleRow(id string, at time.Time) txRow {
	return txRow{
		ID: id, AmountCents: 2600, Direction: "expense", CategoryID: 1,
		Note: "晚饭 兰州拉面加蛋", OccurredAt: at, PaymentMethod: "alipay",
		CreatedAt: created, UpdatedAt: created,
	}
}

func sampleCreate() createData {
	return createData{
		ID: txID, AmountCents: 2600, Direction: "expense", CategoryID: 1,
		Note: "晚饭 兰州拉面加蛋", OccurredAt: occurred, PaymentMethod: "alipay",
	}
}

// ── Create：幂等 upsert ──────────────────────────────────────────────────────

// TestCreateInsertNew 全新 id → INSERT，响应 createdAt=updatedAt=now、ticketId=null。
func TestCreateInsertNew(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTx)).
		WithArgs(txID, uidA, int64(2600), "expense", int64(1), d.Note, occurred, "alipay", fixedNow, fixedNow).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.create(context.Background(), uidA, d)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.ID != txID || got.TicketID != nil {
		t.Fatalf("got = %+v, want id %s with null ticketId", got, txID)
	}
	if got.CreatedAt != "2026-07-13T10:00:00Z" || got.UpdatedAt != "2026-07-13T10:00:00Z" {
		t.Fatalf("createdAt/updatedAt = %s/%s, want fixed now", got.CreatedAt, got.UpdatedAt)
	}
	mustMeet(t, mock)
}

// TestCreateReplayUpsertsNoDuplicate 同 id 重发 → 走 UPDATE 覆盖（一行数据），
// 绝不产生第二次 INSERT（sqlmock 有序期望：任何多余 INSERT 都会直接失败）。
func TestCreateReplayUpsertsNoDuplicate(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "created_at", "updated_at", "tk_id"}).
			AddRow("expense", created, created, nil))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTx)).
		WithArgs(int64(2600), int64(1), d.Note, occurred, "alipay", fixedNow, txID, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.create(context.Background(), uidA, d)
	if err != nil {
		t.Fatalf("replay create: %v", err)
	}
	// createdAt 保留首次创建时间，updatedAt 变为本次时间。
	if got.CreatedAt != "2026-07-10T03:05:00Z" || got.UpdatedAt != "2026-07-13T10:00:00Z" {
		t.Fatalf("createdAt/updatedAt = %s/%s", got.CreatedAt, got.UpdatedAt)
	}
	mustMeet(t, mock)
}

// TestCreateStaleReplayConflict 携带 updatedAt 且比服务端旧 → 40902，且不落任何写。
func TestCreateStaleReplayConflict(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()
	stale := created.Add(-time.Hour)
	d.ClientUpdated = &stale

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "created_at", "updated_at", "tk_id"}).
			AddRow("expense", created, created, nil))
	mock.ExpectRollback()

	_, err := svc.create(context.Background(), uidA, d)
	wantCode(t, err, apperr.CodeConflict)
	mustMeet(t, mock) // 无任何 ExpectExec：40902 路径零写入
}

// TestCreateReplayNewerClientWins 携带 updatedAt 且不比服务端旧 → LWW 覆盖成功。
func TestCreateReplayNewerClientWins(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()
	newer := created.Add(time.Hour)
	d.ClientUpdated = &newer

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "created_at", "updated_at", "tk_id"}).
			AddRow("expense", created, created, nil))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTx)).
		WithArgs(int64(2600), int64(1), d.Note, occurred, "alipay", fixedNow, txID, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if _, err := svc.create(context.Background(), uidA, d); err != nil {
		t.Fatalf("create: %v", err)
	}
	mustMeet(t, mock)
}

// TestCreateDuplicateIDOtherUser 主键被他人占用 → MySQL 1062 → 40902（不泄漏归属）。
func TestCreateDuplicateIDOtherUser(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnError(sql.ErrNoRows) // 本人名下无此 id
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTx)).
		WillReturnError(&mysql.MySQLError{Number: mysqlErrDuplicateEntry, Message: "Duplicate entry"})
	mock.ExpectRollback()

	_, err := svc.create(context.Background(), uidA, sampleCreate())
	wantCode(t, err, apperr.CodeConflict)
	mustMeet(t, mock)
}

// TestCreateReplayDirectionChangeForbidden 重放改 direction → 40001（与 PATCH 同规则）。
func TestCreateReplayDirectionChangeForbidden(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()
	d.Direction = "income"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "created_at", "updated_at", "tk_id"}).
			AddRow("expense", created, created, nil))
	mock.ExpectRollback()

	_, err := svc.create(context.Background(), uidA, d)
	wantCode(t, err, apperr.CodeInvalidParam)
	mustMeet(t, mock)
}

// TestCreateReplayKeepsTicketLink 重放票据关联的交易：响应 ticketId 保持非空（只读派生）。
func TestCreateReplayKeepsTicketLink(t *testing.T) {
	svc, mock := newTestService(t)
	const ticketID = "2afe70af-2033-4e5d-b8d4-43edad37fdcb"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "created_at", "updated_at", "tk_id"}).
			AddRow("expense", created, created, ticketID))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTx)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.create(context.Background(), uidA, sampleCreate())
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.TicketID == nil || *got.TicketID != ticketID {
		t.Fatalf("ticketId = %v, want %s", got.TicketID, ticketID)
	}
	mustMeet(t, mock)
}

// ── List：keyset 分页 ────────────────────────────────────────────────────────

const listOrder = " ORDER BY tx.occurred_at DESC, tx.id DESC LIMIT ?"

// TestListPaginationHasNext 多取 1 行判页：limit=2 返回 3 行 → 2 条 + nextCursor 指向第 2 条。
func TestListPaginationHasNext(t *testing.T) {
	svc, mock := newTestService(t)
	r1 := sampleRow("a1b041cd-b29c-4f45-a48c-9294b6a9bf8f", occurred.Add(2*time.Hour))
	r2 := sampleRow("b1b041cd-b29c-4f45-a48c-9294b6a9bf8f", occurred.Add(time.Hour))
	r3 := sampleRow("c1b041cd-b29c-4f45-a48c-9294b6a9bf8f", occurred)

	q := sqlSelectTx + " WHERE tx.user_id = ? AND tx.deleted_at IS NULL" + listOrder
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(uidA, 3).
		WillReturnRows(addRow(addRow(addRow(txRowsCols(), r1), r2), r3))

	res, err := svc.list(context.Background(), uidA, listFilter{limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(res.Items))
	}
	if res.NextCursor == nil {
		t.Fatal("nextCursor = nil, want cursor for page 2")
	}
	if want := encodeCursor(r2.OccurredAt, r2.ID); *res.NextCursor != want {
		t.Fatalf("nextCursor = %q, want %q", *res.NextCursor, want)
	}
	mustMeet(t, mock)
}

// TestListPaginationExactLimit 恰好 limit 行（无第 limit+1 行）→ nextCursor 为 null。
func TestListPaginationExactLimit(t *testing.T) {
	svc, mock := newTestService(t)
	r1 := sampleRow("a1b041cd-b29c-4f45-a48c-9294b6a9bf8f", occurred.Add(time.Hour))
	r2 := sampleRow("b1b041cd-b29c-4f45-a48c-9294b6a9bf8f", occurred)

	q := sqlSelectTx + " WHERE tx.user_id = ? AND tx.deleted_at IS NULL" + listOrder
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(uidA, 3).
		WillReturnRows(addRow(addRow(txRowsCols(), r1), r2))

	res, err := svc.list(context.Background(), uidA, listFilter{limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Items) != 2 || res.NextCursor != nil {
		t.Fatalf("items=%d nextCursor=%v, want 2 items and nil cursor", len(res.Items), res.NextCursor)
	}
	mustMeet(t, mock)
}

// TestListPaginationEmptyNextPage 用上页游标翻页且无更多数据 → items=[]（非 null）、nextCursor=null；
// 同时验证游标经 encode→decode 后作为断点参数原样进入 SQL（游标稳定性）。
func TestListPaginationEmptyNextPage(t *testing.T) {
	svc, mock := newTestService(t)
	last := sampleRow("b1b041cd-b29c-4f45-a48c-9294b6a9bf8f", occurred)

	curTime, curID, err := decodeCursor(encodeCursor(last.OccurredAt, last.ID))
	if err != nil {
		t.Fatalf("cursor roundtrip: %v", err)
	}

	q := sqlSelectTx + " WHERE tx.user_id = ? AND tx.deleted_at IS NULL" +
		" AND (tx.occurred_at < ? OR (tx.occurred_at = ? AND tx.id < ?))" + listOrder
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(uidA, last.OccurredAt, last.OccurredAt, last.ID, 3).
		WillReturnRows(txRowsCols())

	res, err := svc.list(context.Background(), uidA,
		listFilter{limit: 2, hasCursor: true, curTime: curTime, curID: curID})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if res.Items == nil || len(res.Items) != 0 {
		t.Fatalf("items = %v, want empty non-nil slice", res.Items)
	}
	if res.NextCursor != nil {
		t.Fatalf("nextCursor = %q, want nil", *res.NextCursor)
	}
	mustMeet(t, mock)
}

// TestListMonthFilterBoundaries 月过滤是 UTC 半开区间 [月初, 下月初)。
func TestListMonthFilterBoundaries(t *testing.T) {
	svc, mock := newTestService(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	q := sqlSelectTx + " WHERE tx.user_id = ? AND tx.deleted_at IS NULL" +
		" AND tx.occurred_at >= ? AND tx.occurred_at < ?" + listOrder
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(uidA, start, end, 51).
		WillReturnRows(txRowsCols())

	_, err := svc.list(context.Background(), uidA,
		listFilter{limit: 50, hasMonth: true, monthStart: start, monthEnd: end})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	mustMeet(t, mock)
}

// TestListAllFilters 组合过滤子句顺序固定：month → categoryId → direction → cursor。
func TestListAllFilters(t *testing.T) {
	svc, mock := newTestService(t)
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	q := sqlSelectTx + " WHERE tx.user_id = ? AND tx.deleted_at IS NULL" +
		" AND tx.occurred_at >= ? AND tx.occurred_at < ?" +
		" AND tx.category_id = ? AND tx.direction = ?" +
		" AND (tx.occurred_at < ? OR (tx.occurred_at = ? AND tx.id < ?))" + listOrder
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(uidA, start, end, int64(3), "expense", occurred, occurred, txID, 21).
		WillReturnRows(txRowsCols())

	_, err := svc.list(context.Background(), uidA, listFilter{
		limit: 20, hasMonth: true, monthStart: start, monthEnd: end,
		categoryID: 3, direction: "expense",
		hasCursor: true, curTime: occurred, curID: txID,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	mustMeet(t, mock)
}

// ── Patch ───────────────────────────────────────────────────────────────────

// TestPatchUpdatesFields 部分更新：动态 SET 固定字段序 + bump updated_at，联动响应值。
func TestPatchUpdatesFields(t *testing.T) {
	svc, mock := newTestService(t)
	amount := int64(9900)
	note := "改：加了卤蛋"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, uidA).
		WillReturnRows(addRow(txRowsCols(), sampleRow(txID, occurred)))
	mock.ExpectExec(regexp.QuoteMeta(
		"UPDATE transactions SET amount_cents = ?, note = ?, updated_at = ? WHERE id = ? AND user_id = ?")).
		WithArgs(amount, note, fixedNow, txID, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.patch(context.Background(), uidA, txID, patchData{AmountCents: &amount, Note: &note})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got.AmountCents != 9900 || got.Note != note || got.UpdatedAt != "2026-07-13T10:00:00Z" {
		t.Fatalf("got = %+v", got)
	}
	mustMeet(t, mock)
}

// TestPatchDirectionChangeForbidden PATCH 改 direction → 40001（契约 v1.1）。
func TestPatchDirectionChangeForbidden(t *testing.T) {
	svc, mock := newTestService(t)
	income := "income"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, uidA).
		WillReturnRows(addRow(txRowsCols(), sampleRow(txID, occurred))) // direction=expense
	mock.ExpectRollback()

	_, err := svc.patch(context.Background(), uidA, txID, patchData{Direction: &income})
	wantCode(t, err, apperr.CodeInvalidParam)
	mustMeet(t, mock)
}

// TestPatchSameDirectionNoop direction 传了但与现值相同 → 允许（非「改」），且不进 SET。
func TestPatchSameDirectionNoop(t *testing.T) {
	svc, mock := newTestService(t)
	expense := "expense"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, uidA).
		WillReturnRows(addRow(txRowsCols(), sampleRow(txID, occurred)))
	mock.ExpectCommit() // 无 UPDATE：没有实际变更

	got, err := svc.patch(context.Background(), uidA, txID, patchData{Direction: &expense})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got.Direction != "expense" || got.UpdatedAt != "2026-07-10T03:05:00Z" {
		t.Fatalf("got = %+v, want unchanged entity", got)
	}
	mustMeet(t, mock)
}

// TestPatchOtherUsersRow 他人（或不存在/已删）的交易：user_id 条件使查询无行 → 40401 不泄漏。
func TestPatchOtherUsersRow(t *testing.T) {
	svc, mock := newTestService(t)
	const uidB int64 = 8
	amount := int64(1)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, uidB). // B 的 uid 进查询 → A 的行不可见
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := svc.patch(context.Background(), uidB, txID, patchData{AmountCents: &amount})
	wantCode(t, err, apperr.CodeNotFound)
	mustMeet(t, mock)
}

// ── Delete ──────────────────────────────────────────────────────────────────

// TestDeleteSoftDeletes 无票据关联 → 软删（deleted_at + bump updated_at）。
func TestDeleteSoftDeletes(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, uidA).
		WillReturnRows(addRow(txRowsCols(), sampleRow(txID, occurred)))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTx)).
		WithArgs(fixedNow, fixedNow, txID, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := svc.remove(context.Background(), uidA, txID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	mustMeet(t, mock)
}

// TestDeleteTicketLinkedForbidden ticketId 非空 → 40001「请从票夹删除该票据」，零写入。
func TestDeleteTicketLinkedForbidden(t *testing.T) {
	svc, mock := newTestService(t)
	r := sampleRow(txID, occurred)
	r.TicketID = sql.NullString{String: "2afe70af-2033-4e5d-b8d4-43edad37fdcb", Valid: true}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, uidA).
		WillReturnRows(addRow(txRowsCols(), r))
	mock.ExpectRollback()

	err := svc.remove(context.Background(), uidA, txID)
	wantCode(t, err, apperr.CodeInvalidParam)
	var ae *apperr.Error
	if errors.As(err, &ae) && ae.Msg != "请从票夹删除该票据" {
		t.Fatalf("msg = %q, want 契约原文「请从票夹删除该票据」", ae.Msg)
	}
	mustMeet(t, mock)
}

// TestDeleteNotFound 不存在/已删/他人的交易 → 40401。
func TestDeleteNotFound(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTxForUpdate)).
		WithArgs(txID, uidA).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	wantCode(t, svc.remove(context.Background(), uidA, txID), apperr.CodeNotFound)
	mustMeet(t, mock)
}
