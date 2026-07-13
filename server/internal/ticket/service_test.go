package ticket

// service 层单测：与 transaction 模块同款 go-sqlmock 方案（本机无 Docker/MySQL），
// regexp.QuoteMeta 复用 service.go 的 SQL 常量，防实现/断言漂移。
// 真库执行路径由 Wave 3 集成阶段 docker compose 冒烟覆盖。
//
// S4 DoD 核心：票↔交易联动一致性 —— create 同事务双 insert、replay 同事务双 upsert、
// patch 金额同事务改交易、delete 同事务双软删；user_id 隔离靠 SQL 常量自带条件 + WithArgs 含 uid。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/apperr"
)

const (
	uidA   int64 = 7
	tkID         = "3f1b6d2a-9c41-4e0f-8a52-7d0e94c6b1aa"
	txIDA        = "c0fed131-7235-4d6b-814e-bf3786bdc01e"
	otherT       = "a1a1a1a1-b2b2-4c3c-8d4d-e5e5e5e5e5e5"
)

var (
	fixedNow = time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	evtTime  = time.Date(2026, 7, 10, 12, 30, 0, 0, time.UTC)
	occTime  = time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	created  = time.Date(2026, 7, 1, 8, 0, 0, 0, time.UTC)
	updated  = time.Date(2026, 7, 5, 8, 0, 0, 0, time.UTC)
)

func newTestService(t *testing.T) (*service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &service{db: db, secret: "test-secret", now: func() time.Time { return fixedNow }}, mock
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
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want apperr %d", err, code)
	}
	if ae.Code != code {
		t.Fatalf("code = %d, want %d", ae.Code, code)
	}
}

func joinCols() []string {
	return []string{"id", "transaction_id", "kind", "title", "venue", "event_time", "seat",
		"extra", "rating", "memo", "created_at", "updated_at",
		"amount_cents", "category_id", "payment_method"}
}

func sampleCreate() createData {
	return createData{
		ID: tkID, Kind: "movie", Title: "沙丘2", Venue: "万达影城",
		EventTime: evtTime, Seat: "5排8座",
		Extra:  map[string]string{"cinema": "万达影城CBD店"},
		Rating: 5, Memo: "IMAX", AmountCents: 4500, CategoryID: 3,
		PaymentMethod: "wechat", OccurredAt: occTime,
	}
}

// ── create：全新 id，事务内 insert transactions → insert tickets ────────────────

func TestCreateInsertsTicketAndTransactionAtomically(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTransaction)).
		WithArgs(sqlmock.AnyArg(), uidA, int64(4500), "expense", int64(3), "沙丘2", occTime, "wechat", fixedNow, fixedNow).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTicket)).
		WithArgs(tkID, uidA, sqlmock.AnyArg(), "movie", "沙丘2", "万达影城", evtTime, "5排8座",
			[]byte(`{"cinema":"万达影城CBD店"}`), 5, "IMAX", fixedNow, fixedNow).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	got, err := svc.create(context.Background(), uidA, d)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if got.ID != tkID || got.Transaction.AmountCents != 4500 || got.Transaction.CategoryID != 3 {
		t.Fatalf("ticket = %+v", got)
	}
	if !uuidRe.MatchString(got.Transaction.ID) {
		t.Fatalf("transaction id %q not a UUID", got.Transaction.ID)
	}
	if got.Extra["cinema"] != "万达影城CBD店" || got.Extra["hall"] != "" || got.Extra["filmFormat"] != "" {
		t.Fatalf("extra not filled to full shape: %v", got.Extra)
	}
	if got.CreatedAt != rfc3339(fixedNow) || got.UpdatedAt != rfc3339(fixedNow) {
		t.Fatalf("timestamps = %s / %s", got.CreatedAt, got.UpdatedAt)
	}
	mustMeet(t, mock)
}

// ── create：同 id 重放（LWW upsert，票 + 交易同事务覆盖并清墓碑）──────────────

func TestCreateReplayUpsertsBoth(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"transaction_id", "created_at", "updated_at"}).
			AddRow(txIDA, created, updated))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTransaction)).
		WithArgs(int64(4500), int64(3), "wechat", "沙丘2", occTime, fixedNow, txIDA, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTicket)).
		WithArgs("movie", "沙丘2", "万达影城", evtTime, "5排8座",
			[]byte(`{"cinema":"万达影城CBD店"}`), 5, "IMAX", fixedNow, tkID, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlUnbindAllAttachments)).
		WithArgs(uidA, tkID).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	got, err := svc.create(context.Background(), uidA, d)
	if err != nil {
		t.Fatalf("create replay: %v", err)
	}
	if got.Transaction.ID != txIDA {
		t.Fatalf("transaction id = %s, want existing %s", got.Transaction.ID, txIDA)
	}
	if got.CreatedAt != rfc3339(created) || got.UpdatedAt != rfc3339(fixedNow) {
		t.Fatalf("createdAt/updatedAt = %s / %s", got.CreatedAt, got.UpdatedAt)
	}
	mustMeet(t, mock)
}

func TestCreateStaleReplayConflict(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()
	stale := updated.Add(-time.Hour)
	d.ClientUpdated = &stale

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"transaction_id", "created_at", "updated_at"}).
			AddRow(txIDA, created, updated))
	mock.ExpectRollback()

	_, err := svc.create(context.Background(), uidA, d)
	wantCode(t, err, apperr.CodeConflict)
	mustMeet(t, mock)
}

// 同 id 被他人占用 / 并发重放 → 主键冲突 → 40902，不泄漏归属。
func TestCreateDuplicateKeyConflict(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTransaction)).
		WithArgs(sqlmock.AnyArg(), uidA, int64(4500), "expense", int64(3), "沙丘2", occTime, "wechat", fixedNow, fixedNow).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTicket)).
		WithArgs(tkID, uidA, sqlmock.AnyArg(), "movie", "沙丘2", "万达影城", evtTime, "5排8座",
			[]byte(`{"cinema":"万达影城CBD店"}`), 5, "IMAX", fixedNow, fixedNow).
		WillReturnError(&mysql.MySQLError{Number: mysqlErrDuplicateEntry, Message: "Duplicate entry"})
	mock.ExpectRollback()

	_, err := svc.create(context.Background(), uidA, d)
	wantCode(t, err, apperr.CodeConflict)
	mustMeet(t, mock)
}

// 附件已绑其他票 → 40001，事务回滚，不产生任何写。
func TestCreateAttachmentBoundElsewhere(t *testing.T) {
	svc, mock := newTestService(t)
	d := sampleCreate()
	d.AttachmentIDs = []int64{5}

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(sqlLockAttachmentsIn, "?"))).
		WithArgs(uidA, int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_id", "file_path", "thumb_path", "w", "h"}).
			AddRow(5, otherT, "7/a.jpg", "7/a_thumb.jpg", 100, 100))
	mock.ExpectRollback()

	_, err := svc.create(context.Background(), uidA, d)
	wantCode(t, err, apperr.CodeInvalidParam)
	mustMeet(t, mock)
}

// ── patch：金额类字段同事务同步改关联交易 ───────────────────────────────────────

func TestPatchAmountSyncsTransaction(t *testing.T) {
	svc, mock := newTestService(t)
	amount := int64(9900)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTicketForUpdate)).
		WithArgs(tkID, uidA).
		WillReturnRows(sqlmock.NewRows(joinCols()).
			AddRow(tkID, txIDA, "movie", "沙丘2", "万达影城", evtTime, "5排8座",
				[]byte(`{"cinema":"万达影城CBD店"}`), 5, "IMAX", created, updated,
				int64(4500), int64(3), "wechat"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE tickets SET updated_at = ? WHERE id = ? AND user_id = ?")).
		WithArgs(fixedNow, tkID, uidA).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE transactions SET amount_cents = ?, updated_at = ? WHERE id = ? AND user_id = ?")).
		WithArgs(amount, fixedNow, txIDA, uidA).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectAttachmentsByTicket)).
		WithArgs(uidA, tkID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "file_path", "thumb_path", "w", "h"}))
	mock.ExpectCommit()

	got, err := svc.patch(context.Background(), uidA, tkID, patchData{AmountCents: &amount})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got.Transaction.AmountCents != amount {
		t.Fatalf("amountCents = %d, want %d", got.Transaction.AmountCents, amount)
	}
	if got.UpdatedAt != rfc3339(fixedNow) {
		t.Fatalf("updatedAt = %s, want bumped to now", got.UpdatedAt)
	}
	mustMeet(t, mock)
}

func TestPatchTicketFieldOnlyLeavesTransactionUntouched(t *testing.T) {
	svc, mock := newTestService(t)
	title := "沙丘2 重映"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTicketForUpdate)).
		WithArgs(tkID, uidA).
		WillReturnRows(sqlmock.NewRows(joinCols()).
			AddRow(tkID, txIDA, "movie", "沙丘2", "万达影城", evtTime, "5排8座",
				[]byte(`{}`), 5, "IMAX", created, updated,
				int64(4500), int64(3), "wechat"))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE tickets SET title = ?, updated_at = ? WHERE id = ? AND user_id = ?")).
		WithArgs(title, fixedNow, tkID, uidA).WillReturnResult(sqlmock.NewResult(0, 1))
	// 无 transactions UPDATE：仅票字段变更不得触碰交易行。
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectAttachmentsByTicket)).
		WithArgs(uidA, tkID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "file_path", "thumb_path", "w", "h"}))
	mock.ExpectCommit()

	got, err := svc.patch(context.Background(), uidA, tkID, patchData{Title: &title})
	if err != nil {
		t.Fatalf("patch: %v", err)
	}
	if got.Title != title || got.Transaction.AmountCents != 4500 {
		t.Fatalf("ticket = %+v", got)
	}
	mustMeet(t, mock)
}

// 不存在 / 已删 / 他人的票，一律 40401 不泄漏归属。
func TestPatchNotFound(t *testing.T) {
	svc, mock := newTestService(t)
	title := "x"

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTicketForUpdate)).
		WithArgs(tkID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	_, err := svc.patch(context.Background(), uidA, tkID, patchData{Title: &title})
	wantCode(t, err, apperr.CodeNotFound)
	mustMeet(t, mock)
}

// ── remove：同事务双软删（票 + 关联交易），bump updated_at 供 sync 墓碑下发 ──────

func TestRemoveSoftDeletesTicketAndTransaction(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT transaction_id FROM tickets WHERE id = ? AND user_id = ? AND deleted_at IS NULL FOR UPDATE")).
		WithArgs(tkID, uidA).
		WillReturnRows(sqlmock.NewRows([]string{"transaction_id"}).AddRow(txIDA))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTicket)).
		WithArgs(fixedNow, fixedNow, tkID, uidA).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTransaction)).
		WithArgs(fixedNow, fixedNow, txIDA, uidA).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := svc.remove(context.Background(), uidA, tkID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	mustMeet(t, mock)
}

func TestRemoveNotFound(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(
		"SELECT transaction_id FROM tickets WHERE id = ? AND user_id = ? AND deleted_at IS NULL FOR UPDATE")).
		WithArgs(tkID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	err := svc.remove(context.Background(), uidA, tkID)
	wantCode(t, err, apperr.CodeNotFound)
	mustMeet(t, mock)
}

// ── get / list ──────────────────────────────────────────────────────────────

func TestGetNotFound(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectQuery(regexp.QuoteMeta(sqlGetTicket)).
		WithArgs(tkID, uidA).WillReturnError(sql.ErrNoRows)

	_, err := svc.get(context.Background(), uidA, tkID)
	wantCode(t, err, apperr.CodeNotFound)
	mustMeet(t, mock)
}

func TestListPaginationNextCursor(t *testing.T) {
	svc, mock := newTestService(t)
	id2 := "22222222-2222-4222-8222-222222222222"
	t1 := evtTime
	t2 := evtTime.Add(-time.Hour)
	t3 := evtTime.Add(-2 * time.Hour)

	q := sqlSelectTicketJoin + " WHERE t.user_id = ? AND t.deleted_at IS NULL ORDER BY t.event_time DESC, t.id DESC LIMIT ?"
	rows := sqlmock.NewRows(joinCols())
	for i, rc := range []struct {
		id string
		et time.Time
	}{{tkID, t1}, {id2, t2}, {otherT, t3}} {
		rows.AddRow(rc.id, txIDA, "movie", fmt.Sprintf("片%d", i), "", rc.et, "",
			[]byte(`{}`), 0, "", created, updated, int64(1000), int64(3), "other")
	}
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs(uidA, 3).WillReturnRows(rows) // limit+1 探测下一页
	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(sqlListAttachmentsIn, "?, ?"))).
		WithArgs(uidA, tkID, id2).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_id", "file_path", "thumb_path", "w", "h"}))

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
	ct, cid, err := decodeCursor(*res.NextCursor)
	if err != nil || !ct.Equal(t2) || cid != id2 {
		t.Fatalf("cursor decodes to (%v, %s), want (%v, %s)", ct, cid, t2, id2)
	}
	mustMeet(t, mock)
}

func TestListLastPageNoCursor(t *testing.T) {
	svc, mock := newTestService(t)

	q := sqlSelectTicketJoin + " WHERE t.user_id = ? AND t.deleted_at IS NULL ORDER BY t.event_time DESC, t.id DESC LIMIT ?"
	rows := sqlmock.NewRows(joinCols()).
		AddRow(tkID, txIDA, "movie", "唯一一张", "", evtTime, "",
			[]byte(`{}`), 0, "", created, updated, int64(1000), int64(3), "other")
	mock.ExpectQuery(regexp.QuoteMeta(q)).WithArgs(uidA, 3).WillReturnRows(rows)
	mock.ExpectQuery(regexp.QuoteMeta(fmt.Sprintf(sqlListAttachmentsIn, "?"))).
		WithArgs(uidA, tkID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_id", "file_path", "thumb_path", "w", "h"}))

	res, err := svc.list(context.Background(), uidA, listFilter{limit: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(res.Items) != 1 || res.NextCursor != nil {
		t.Fatalf("items = %d, nextCursor = %v; want 1, nil", len(res.Items), res.NextCursor)
	}
	mustMeet(t, mock)
}

// TestListPaginationEmptyNextPage 用上页游标翻页且无更多数据 → items=[]（非 null）、nextCursor=null；
// 同时验证游标经 encode→decode 后作为 (event_time, id) 双键断点原样进入 SQL（tie-breaker 写错会翻页丢票/重票）。
func TestListPaginationEmptyNextPage(t *testing.T) {
	svc, mock := newTestService(t)

	curTime, curID, err := decodeCursor(encodeCursor(evtTime, tkID))
	if err != nil {
		t.Fatalf("cursor roundtrip: %v", err)
	}

	q := sqlSelectTicketJoin + " WHERE t.user_id = ? AND t.deleted_at IS NULL" +
		" AND (t.event_time < ? OR (t.event_time = ? AND t.id < ?))" +
		" ORDER BY t.event_time DESC, t.id DESC LIMIT ?"
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(uidA, evtTime, evtTime, tkID, 3).
		WillReturnRows(sqlmock.NewRows(joinCols()))

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

// TestListYearFilterBoundaries year 过滤是 UTC 半开区间 [年初, 次年初)。
func TestListYearFilterBoundaries(t *testing.T) {
	svc, mock := newTestService(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	q := sqlSelectTicketJoin + " WHERE t.user_id = ? AND t.deleted_at IS NULL" +
		" AND t.event_time >= ? AND t.event_time < ?" +
		" ORDER BY t.event_time DESC, t.id DESC LIMIT ?"
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(uidA, start, end, 21).
		WillReturnRows(sqlmock.NewRows(joinCols()))

	_, err := svc.list(context.Background(), uidA, listFilter{limit: 20, year: 2026})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	mustMeet(t, mock)
}

// TestListAllFilters 组合过滤子句顺序固定：kind → year → cursor。
func TestListAllFilters(t *testing.T) {
	svc, mock := newTestService(t)
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)

	q := sqlSelectTicketJoin + " WHERE t.user_id = ? AND t.deleted_at IS NULL" +
		" AND t.kind = ?" +
		" AND t.event_time >= ? AND t.event_time < ?" +
		" AND (t.event_time < ? OR (t.event_time = ? AND t.id < ?))" +
		" ORDER BY t.event_time DESC, t.id DESC LIMIT ?"
	mock.ExpectQuery(regexp.QuoteMeta(q)).
		WithArgs(uidA, "movie", start, end, evtTime, evtTime, tkID, 21).
		WillReturnRows(sqlmock.NewRows(joinCols()))

	_, err := svc.list(context.Background(), uidA, listFilter{
		limit: 20, kind: "movie", year: 2026,
		hasCursor: true, curTime: evtTime, curID: tkID,
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	mustMeet(t, mock)
}
