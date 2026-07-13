package sync

// service 层单测：本机无 Docker/MySQL（同 platform/db 约束），用 go-sqlmock 精确断言
// SQL 语句 + 参数（regexp.QuoteMeta 复用 service.go 的 SQL 常量，防实现/断言漂移）。
// 真库执行路径由集成阶段 docker compose 冒烟覆盖。
//
// user_id 隔离的断言方式：所有 WithArgs 均含 uid，且 SQL 常量本身带 user_id 条件；
// 他人的行在 push 里表现为 ErrNoRows（删除 → 幂等 no-op；upsert → 主键冲突 40902）。

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	"github.com/go-sql-driver/mysql"

	"piaoju/internal/platform/apperr"
)

const (
	uidA  int64 = 7
	txID        = "c0fed131-7235-4d6b-814e-bf3786bdc01e"
	txID2       = "d1a0b2c3-4d5e-4f60-8123-456789abcdef"
	tkID        = "9b6c1f2e-3a4d-4b5c-8d6e-7f8091a2b3c4"
)

var (
	fixedNow = time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	occurred = time.Date(2026, 7, 10, 3, 4, 5, 0, time.UTC)
	eventAt  = time.Date(2026, 7, 11, 12, 30, 0, 0, time.UTC)
	created  = time.Date(2026, 7, 10, 3, 5, 0, 0, time.UTC)

	// serverUpdated 服务端现有版本时间；客户端更旧 → stale，更新 → applied。
	serverUpdated = time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	clientOlder   = time.Date(2026, 7, 11, 8, 0, 0, 0, time.UTC)
	clientNewer   = time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
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

func wantResult(t *testing.T, got changeResult, id, status string, code int) {
	t.Helper()
	if got.ID != id || got.Status != status || got.Code != code {
		t.Fatalf("result = %+v, want {ID:%s Status:%s Code:%d}", got, id, status, code)
	}
}

// ── push 载荷构造 ────────────────────────────────────────────────────────────

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

func txUpsertChange(t *testing.T, clientUpdated time.Time) rawChange {
	t.Helper()
	return rawChange{
		Entity: "transaction", Op: "upsert",
		ClientUpdatedAt: clientUpdated.Format(time.RFC3339),
		Payload: rawJSON(t, map[string]any{
			"id": txID, "amountCents": 2600, "direction": "expense", "categoryId": 1,
			"note": "晚饭 兰州拉面加蛋", "occurredAt": occurred.Format(time.RFC3339),
			"paymentMethod": "alipay",
		}),
	}
}

func ticketUpsertChange(t *testing.T, clientUpdated time.Time, attachmentIDs []int64) rawChange {
	t.Helper()
	p := map[string]any{
		"id": tkID, "transactionId": txID, "kind": "movie", "title": "沙丘2",
		"venue": "万达影城", "eventTime": eventAt.Format(time.RFC3339), "seat": "5排8座",
		"extra":  map[string]string{"cinema": "万达", "hall": "3号厅", "filmFormat": "IMAX"},
		"rating": 5, "memo": "值回票价",
		"amountCents": 4500, "categoryId": 3, "paymentMethod": "wechat",
		"occurredAt": occurred.Format(time.RFC3339),
	}
	if attachmentIDs != nil {
		p["attachmentIds"] = attachmentIDs
	}
	return rawChange{
		Entity: "ticket", Op: "upsert",
		ClientUpdatedAt: clientUpdated.Format(time.RFC3339),
		Payload:         rawJSON(t, p),
	}
}

func deleteChange(t *testing.T, entity, id string, clientUpdated time.Time) rawChange {
	t.Helper()
	return rawChange{
		Entity: entity, Op: "delete",
		ClientUpdatedAt: clientUpdated.Format(time.RFC3339),
		Payload:         rawJSON(t, map[string]any{"id": id}),
	}
}

// txMetaRows 与 sqlSelectTxMeta 的列序一一对应。
func txMetaRows(direction string, updatedAt time.Time, deletedAt any, ticketID any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"direction", "created_at", "updated_at", "deleted_at", "tk_id"}).
		AddRow(direction, created, updatedAt, deletedAt, ticketID)
}

// ticketMetaRows 与 sqlSelectTicketMeta 的列序一一对应。
func ticketMetaRows(transactionID string, updatedAt time.Time, deletedAt any) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"transaction_id", "created_at", "updated_at", "deleted_at"}).
		AddRow(transactionID, created, updatedAt, deletedAt)
}

// ── push：transaction upsert ────────────────────────────────────────────────

func TestPushTxUpsertInsertsNewRow(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTx)).
		WithArgs(txID, uidA, int64(2600), "expense", int64(1), "晚饭 兰州拉面加蛋", occurred, "alipay", fixedNow, fixedNow).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res := svc.push(context.Background(), uidA, []rawChange{txUpsertChange(t, clientNewer)})
	if len(res.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(res.Results))
	}
	wantResult(t, res.Results[0], txID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// 幂等重放：同 id 再次 upsert（客户端版本不旧）→ 整体覆盖 + 清墓碑，仍是 applied。
func TestPushTxUpsertIdempotentReplay(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(txMetaRows("expense", serverUpdated, nil, nil))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTx)).
		WithArgs(int64(2600), int64(1), "晚饭 兰州拉面加蛋", occurred, "alipay", fixedNow, txID, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res := svc.push(context.Background(), uidA, []rawChange{txUpsertChange(t, clientNewer)})
	wantResult(t, res.Results[0], txID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// LWW：clientUpdatedAt < 服务端 updated_at → stale，不写入（事务回滚），服务端版本随 pull 下发。
func TestPushTxUpsertStale(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(txMetaRows("expense", serverUpdated, nil, nil))
	mock.ExpectRollback() // 无任何 UPDATE

	res := svc.push(context.Background(), uidA, []rawChange{txUpsertChange(t, clientOlder)})
	wantResult(t, res.Results[0], txID, statusStale, apperr.CodeConflict)
	mustMeet(t, mock)
}

// clientUpdatedAt == 服务端 updated_at → 不算 stale（同版本重放幂等写入）。
func TestPushTxUpsertEqualTimestampNotStale(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(txMetaRows("expense", serverUpdated, nil, nil))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTx)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res := svc.push(context.Background(), uidA, []rawChange{txUpsertChange(t, serverUpdated)})
	wantResult(t, res.Results[0], txID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// direction 建后不可改（与 transaction 模块同规则）→ error 40001。
func TestPushTxUpsertDirectionChangeRejected(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(txMetaRows("income", serverUpdated, nil, nil))
	mock.ExpectRollback()

	res := svc.push(context.Background(), uidA, []rawChange{txUpsertChange(t, clientNewer)})
	wantResult(t, res.Results[0], txID, statusError, apperr.CodeInvalidParam)
	mustMeet(t, mock)
}

// 主键被他人占用（user_id 隔离下 meta 查不到 → INSERT 撞主键）→ error 40902，不泄漏归属。
func TestPushTxUpsertForeignIDConflict(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTx)).
		WillReturnError(&mysql.MySQLError{Number: mysqlErrDuplicateEntry, Message: "Duplicate entry"})
	mock.ExpectRollback()

	res := svc.push(context.Background(), uidA, []rawChange{txUpsertChange(t, clientNewer)})
	wantResult(t, res.Results[0], txID, statusError, apperr.CodeConflict)
	mustMeet(t, mock)
}

// ── push：transaction delete（墓碑）───────────────────────────────────────────

func TestPushTxDeleteTombstone(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(txMetaRows("expense", serverUpdated, nil, nil))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTx)).
		WithArgs(fixedNow, fixedNow, txID, uidA). // updated_at 与 deleted_at 同时 bump
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res := svc.push(context.Background(), uidA, []rawChange{deleteChange(t, "transaction", txID, clientNewer)})
	wantResult(t, res.Results[0], txID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// 不存在（或他人的行 —— user_id 隔离下同为 ErrNoRows）→ 删除幂等 no-op，不写库。
func TestPushTxDeleteMissingIsNoop(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectRollback()

	res := svc.push(context.Background(), uidA, []rawChange{deleteChange(t, "transaction", txID, clientNewer)})
	wantResult(t, res.Results[0], txID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// 已是墓碑 → 不重复 bump updated_at（否则游标抖动、墓碑反复下发）。
func TestPushTxDeleteAlreadyTombstoned(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(txMetaRows("expense", serverUpdated, serverUpdated, nil))
	mock.ExpectRollback()

	res := svc.push(context.Background(), uidA, []rawChange{deleteChange(t, "transaction", txID, clientNewer)})
	wantResult(t, res.Results[0], txID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// 票据关联的交易必须从票夹删（契约 v1.1）→ error 40001。
func TestPushTxDeleteLinkedToTicketRejected(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(txMetaRows("expense", serverUpdated, nil, tkID))
	mock.ExpectRollback()

	res := svc.push(context.Background(), uidA, []rawChange{deleteChange(t, "transaction", txID, clientNewer)})
	wantResult(t, res.Results[0], txID, statusError, apperr.CodeInvalidParam)
	mustMeet(t, mock)
}

// 删除的 LWW：服务端有更新版本 → stale（不能用旧的离线删除覆盖新编辑）。
func TestPushTxDeleteStale(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).
		WillReturnRows(txMetaRows("expense", serverUpdated, nil, nil))
	mock.ExpectRollback()

	res := svc.push(context.Background(), uidA, []rawChange{deleteChange(t, "transaction", txID, clientOlder)})
	wantResult(t, res.Results[0], txID, statusStale, apperr.CodeConflict)
	mustMeet(t, mock)
}

// ── push：ticket upsert（票↔交易联动）───────────────────────────────────────

// 建票：同事务先建交易（id = payload.transactionId、direction 恒 expense、note = title 快照）再建票。
func TestPushTicketUpsertInsertsLinkedTransaction(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTx)).
		WithArgs(txID, uidA, int64(4500), "expense", int64(3), "沙丘2", occurred, "wechat", fixedNow, fixedNow).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTicket)).
		WithArgs(tkID, uidA, txID, "movie", "沙丘2", "万达影城", eventAt, "5排8座",
			sqlmock.AnyArg(), 5, "值回票价", fixedNow, fixedNow).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res := svc.push(context.Background(), uidA, []rawChange{ticketUpsertChange(t, clientNewer, nil)})
	wantResult(t, res.Results[0], tkID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// 重放：联动交易主键以库中现值为准，payload.transactionId 不得覆盖（防交易分裂）。
func TestPushTicketReplayKeepsServerTransactionID(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).
		WillReturnRows(ticketMetaRows(txID2, serverUpdated, nil)) // 库中是 txID2，payload 带的是 txID
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTicketTx)).
		WithArgs(int64(4500), int64(3), "wechat", "沙丘2", occurred, fixedNow, txID2, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpsertTicket)).
		WithArgs("movie", "沙丘2", "万达影城", eventAt, "5排8座",
			sqlmock.AnyArg(), 5, "值回票价", fixedNow, tkID, uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res := svc.push(context.Background(), uidA, []rawChange{ticketUpsertChange(t, clientNewer, nil)})
	wantResult(t, res.Results[0], tkID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

func TestPushTicketUpsertStale(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).
		WillReturnRows(ticketMetaRows(txID, serverUpdated, nil))
	mock.ExpectRollback()

	res := svc.push(context.Background(), uidA, []rawChange{ticketUpsertChange(t, clientOlder, nil)})
	wantResult(t, res.Results[0], tkID, statusStale, apperr.CodeConflict)
	mustMeet(t, mock)
}

// attachmentIds 提供时按全量集合语义重绑（未在列表里的旧绑定解绑，再绑新列表；均带 user_id）。
func TestPushTicketUpsertRebindsAttachments(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTx)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTicket)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE attachments SET ticket_id = NULL WHERE user_id = ? AND ticket_id = ? AND id NOT IN (?, ?)")).
		WithArgs(uidA, tkID, int64(5), int64(6)).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta("UPDATE attachments SET ticket_id = ? WHERE user_id = ? AND id IN (?, ?) AND (ticket_id IS NULL OR ticket_id = ?)")).
		WithArgs(tkID, uidA, int64(5), int64(6), tkID).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	res := svc.push(context.Background(), uidA, []rawChange{ticketUpsertChange(t, clientNewer, []int64{5, 6})})
	wantResult(t, res.Results[0], tkID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// ── push：ticket delete（双墓碑）─────────────────────────────────────────────

func TestPushTicketDeleteSoftDeletesBoth(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).
		WillReturnRows(ticketMetaRows(txID, serverUpdated, nil))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTicket)).
		WithArgs(fixedNow, fixedNow, tkID, uidA).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDeleteTx)).
		WithArgs(fixedNow, fixedNow, txID, uidA).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	res := svc.push(context.Background(), uidA, []rawChange{deleteChange(t, "ticket", tkID, clientNewer)})
	wantResult(t, res.Results[0], tkID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

// ── push：批内隔离 ───────────────────────────────────────────────────────────

// 单条失败不影响其余：非法 payload（error）+ stale + 正常写入，三条各自独立事务、结果按序返回。
func TestPushChangesAreIndependent(t *testing.T) {
	svc, mock := newTestService(t)

	// #2 stale（有查询无写入）
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTicketMeta)).
		WithArgs(tkID, uidA).
		WillReturnRows(ticketMetaRows(txID, serverUpdated, nil))
	mock.ExpectRollback()
	// #3 applied
	mock.ExpectBegin()
	mock.ExpectQuery(regexp.QuoteMeta(sqlSelectTxMeta)).
		WithArgs(txID, uidA).WillReturnError(sql.ErrNoRows)
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertTx)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	bad := rawChange{ // #1：校验失败（amountCents 负数），不碰 DB
		Entity: "transaction", Op: "upsert",
		ClientUpdatedAt: clientNewer.Format(time.RFC3339),
		Payload: rawJSON(t, map[string]any{
			"id": txID2, "amountCents": -1, "direction": "expense", "categoryId": 1,
			"occurredAt": occurred.Format(time.RFC3339),
		}),
	}
	res := svc.push(context.Background(), uidA, []rawChange{
		bad,
		ticketUpsertChange(t, clientOlder, nil),
		txUpsertChange(t, clientNewer),
	})
	if len(res.Results) != 3 {
		t.Fatalf("results = %d, want 3", len(res.Results))
	}
	wantResult(t, res.Results[0], txID2, statusError, apperr.CodeInvalidParam)
	wantResult(t, res.Results[1], tkID, statusStale, apperr.CodeConflict)
	wantResult(t, res.Results[2], txID, statusApplied, apperr.CodeOK)
	mustMeet(t, mock)
}

func TestPushUnsupportedEntityAndOp(t *testing.T) {
	svc, _ := newTestService(t)

	res := svc.push(context.Background(), uidA, []rawChange{
		{Entity: "invoice", Op: "upsert", ClientUpdatedAt: clientNewer.Format(time.RFC3339), Payload: rawJSON(t, map[string]any{"id": txID})},
		{Entity: "transaction", Op: "merge", ClientUpdatedAt: clientNewer.Format(time.RFC3339), Payload: rawJSON(t, map[string]any{"id": txID})},
		{Entity: "transaction", Op: "delete", Payload: rawJSON(t, map[string]any{"id": txID})}, // 缺 clientUpdatedAt
	})
	wantResult(t, res.Results[0], txID, statusError, apperr.CodeUnsupportedEnum)
	wantResult(t, res.Results[1], txID, statusError, apperr.CodeUnsupportedEnum)
	wantResult(t, res.Results[2], txID, statusError, apperr.CodeInvalidParam)
}

// ── pull ────────────────────────────────────────────────────────────────────

func txPullRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "amount_cents", "direction", "category_id", "note", "occurred_at",
		"payment_method", "created_at", "updated_at", "deleted_at", "tk_id",
	})
}

func ticketPullRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "transaction_id", "kind", "title", "venue", "event_time", "seat", "extra",
		"rating", "memo", "created_at", "updated_at", "deleted_at",
		"amount_cents", "category_id", "payment_method",
	})
}

func categoryRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "user_id", "name", "icon", "kind", "sort", "deleted_at"}).
		AddRow(int64(1), nil, "餐饮", "🍜", "expense", 1, nil).
		AddRow(int64(12), uidA, "宠物", "🐱", "expense", 9, serverUpdated) // 自定义 + 墓碑
}

// 首次全量：无 since → 两表 keyset 全扫（各取 limit+1），下发分类快照，nextCursor 指向最后一行。
func TestPullFirstPageDeliversTombstonesAndCategories(t *testing.T) {
	svc, mock := newTestService(t)

	t1 := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(sqlPullTxAll)).
		WithArgs(uidA, 201). // limit+1
		WillReturnRows(txPullRows().
			AddRow(txID, int64(2600), "expense", int64(1), "晚饭", occurred, "alipay", created, t1, t1, nil))
	mock.ExpectQuery(regexp.QuoteMeta(sqlPullTicketsAll)).
		WithArgs(uidA, 201).
		WillReturnRows(ticketPullRows().
			AddRow(tkID, txID2, "movie", "沙丘2", "万达影城", eventAt, "5排8座",
				[]byte(`{"cinema":"万达"}`), 5, "值回票价", created, t2, nil,
				int64(4500), int64(3), "wechat"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, ticket_id, file_path, thumb_path, w, h FROM attachments WHERE user_id = ? AND ticket_id IN (?) ORDER BY ticket_id, id")).
		WithArgs(uidA, tkID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_id", "file_path", "thumb_path", "w", "h"}).
			AddRow(int64(5), tkID, "7/a.jpg", "7/a_thumb.jpg", 1200, 800))
	mock.ExpectQuery(regexp.QuoteMeta(sqlPullCategories)).
		WithArgs(uidA).WillReturnRows(categoryRows())

	res, err := svc.pull(context.Background(), uidA, pullFilter{limit: defaultLimit})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if len(res.Transactions) != 1 || len(res.Tickets) != 1 {
		t.Fatalf("got %d tx / %d tickets, want 1/1", len(res.Transactions), len(res.Tickets))
	}
	if res.HasMore {
		t.Fatal("hasMore = true, want false")
	}
	// 墓碑：交易行 deleted_at 非空 → deletedAt 下发；活票 deletedAt 为 null。
	if res.Transactions[0].DeletedAt == nil || *res.Transactions[0].DeletedAt != rfc3339(t1) {
		t.Fatalf("tombstone deletedAt = %v, want %s", res.Transactions[0].DeletedAt, rfc3339(t1))
	}
	if res.Tickets[0].DeletedAt != nil {
		t.Fatalf("live ticket deletedAt = %v, want nil", *res.Tickets[0].DeletedAt)
	}
	// extra 补全为该 kind 的完整形状；内嵌交易与附件签名 URL 就位。
	if got := res.Tickets[0].Extra; got["cinema"] != "万达" || got["hall"] != "" || got["filmFormat"] != "" {
		t.Fatalf("extra = %v, want full movie shape", got)
	}
	if res.Tickets[0].Transaction.ID != txID2 || len(res.Tickets[0].Attachments) != 1 {
		t.Fatalf("ticket linkage/attachments wrong: %+v", res.Tickets[0])
	}
	// nextCursor = 全序最后一行（票 t2 晚于交易 t1）。
	if res.NextCursor != encodeCursor(t2, tkID) {
		t.Fatalf("nextCursor = %q, want cursor(t2, tkID)", res.NextCursor)
	}
	if len(res.Categories) != 2 || !res.Categories[0].IsSystem || res.Categories[1].DeletedAt == nil {
		t.Fatalf("categories = %+v, want system + tombstoned custom", res.Categories)
	}
	mustMeet(t, mock)
}

// 续页：带 since 游标 → keyset 条件下发；超 limit 的行截断，hasMore=true，
// nextCursor 停在已下发的最后一行（下一页从此断点续，无重无漏）。中间页不重复下发分类。
func TestPullSinceCursorPagination(t *testing.T) {
	svc, mock := newTestService(t)

	cur := time.Date(2026, 7, 12, 7, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 7, 12, 9, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 7, 12, 10, 0, 0, 0, time.UTC)

	mock.ExpectQuery(regexp.QuoteMeta(sqlPullTxSince)).
		WithArgs(uidA, cur, cur, txID, 3). // limit(2)+1；两处 keyset 参数均带 user_id
		WillReturnRows(txPullRows().
			AddRow(txID, int64(2600), "expense", int64(1), "晚饭", occurred, "alipay", created, t1, nil, nil).
			AddRow(txID2, int64(300), "expense", int64(2), "奶茶", occurred, "wechat", created, t3, nil, nil))
	mock.ExpectQuery(regexp.QuoteMeta(sqlPullTicketsSince)).
		WithArgs(uidA, cur, cur, txID, 3).
		WillReturnRows(ticketPullRows().
			AddRow(tkID, txID2, "other", "杂项", "", eventAt, "", []byte(`{}`), 0, "", created, t2, nil,
				int64(4500), int64(3), "wechat"))
	mock.ExpectQuery(regexp.QuoteMeta("SELECT id, ticket_id, file_path, thumb_path, w, h FROM attachments WHERE user_id = ? AND ticket_id IN (?) ORDER BY ticket_id, id")).
		WithArgs(uidA, tkID).
		WillReturnRows(sqlmock.NewRows([]string{"id", "ticket_id", "file_path", "thumb_path", "w", "h"}))

	f := pullFilter{limit: 2, hasCursor: true, curTime: cur, curID: txID, rawCursor: encodeCursor(cur, txID)}
	res, err := svc.pull(context.Background(), uidA, f)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	// 全序 t1(tx) < t2(ticket) < t3(tx)：截断到 2 行 → 1 交易 + 1 票，t3 留给下一页。
	if len(res.Transactions) != 1 || res.Transactions[0].ID != txID {
		t.Fatalf("transactions = %+v, want only txID", res.Transactions)
	}
	if len(res.Tickets) != 1 {
		t.Fatalf("tickets = %d, want 1", len(res.Tickets))
	}
	if !res.HasMore {
		t.Fatal("hasMore = false, want true")
	}
	if res.NextCursor != encodeCursor(t2, tkID) {
		t.Fatalf("nextCursor = %q, want cursor(t2, tkID)", res.NextCursor)
	}
	if len(res.Categories) != 0 {
		t.Fatalf("categories on mid page = %d, want 0", len(res.Categories))
	}
	mustMeet(t, mock)
}

// 增量追平（无新行）：回显入参游标，客户端水位不回退；分类快照仍下发。
func TestPullNoNewRowsEchoesCursor(t *testing.T) {
	svc, mock := newTestService(t)

	cur := time.Date(2026, 7, 12, 7, 0, 0, 0, time.UTC)
	raw := encodeCursor(cur, txID)

	mock.ExpectQuery(regexp.QuoteMeta(sqlPullTxSince)).
		WithArgs(uidA, cur, cur, txID, 201).WillReturnRows(txPullRows())
	mock.ExpectQuery(regexp.QuoteMeta(sqlPullTicketsSince)).
		WithArgs(uidA, cur, cur, txID, 201).WillReturnRows(ticketPullRows())
	mock.ExpectQuery(regexp.QuoteMeta(sqlPullCategories)).
		WithArgs(uidA).WillReturnRows(categoryRows())

	res, err := svc.pull(context.Background(), uidA, pullFilter{
		limit: defaultLimit, hasCursor: true, curTime: cur, curID: txID, rawCursor: raw,
	})
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if res.HasMore || res.NextCursor != raw {
		t.Fatalf("nextCursor = %q hasMore = %v, want echo %q / false", res.NextCursor, res.HasMore, raw)
	}
	if len(res.Transactions) != 0 || len(res.Tickets) != 0 {
		t.Fatal("expected no rows")
	}
	mustMeet(t, mock)
}

// user_id 隔离：pull 的每条语句都带本人 uid（他人数据不可能进结果集）。
func TestPullQueriesAreScopedToUser(t *testing.T) {
	svc, mock := newTestService(t)

	const uidB int64 = 99
	mock.ExpectQuery(regexp.QuoteMeta(sqlPullTxAll)).WithArgs(uidB, 201).WillReturnRows(txPullRows())
	mock.ExpectQuery(regexp.QuoteMeta(sqlPullTicketsAll)).WithArgs(uidB, 201).WillReturnRows(ticketPullRows())
	mock.ExpectQuery(regexp.QuoteMeta(sqlPullCategories)).WithArgs(uidB).WillReturnRows(categoryRows())

	if _, err := svc.pull(context.Background(), uidB, pullFilter{limit: defaultLimit}); err != nil {
		t.Fatalf("pull: %v", err)
	}
	mustMeet(t, mock) // WithArgs 不匹配（漏带/串号 uid）即 fail
}

// ── cursor ──────────────────────────────────────────────────────────────────

func TestCursorRoundTrip(t *testing.T) {
	c := encodeCursor(serverUpdated, txID)
	gotT, gotID, err := decodeCursor(c)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if !gotT.Equal(serverUpdated) || gotID != txID {
		t.Fatalf("round trip = (%v, %s), want (%v, %s)", gotT, gotID, serverUpdated, txID)
	}
	for _, bad := range []string{"!!!", "Zm9v", encodeCursor(serverUpdated, "not-a-uuid")} {
		if _, _, err := decodeCursor(bad); err == nil {
			t.Fatalf("decodeCursor(%q) = nil error, want 40001", bad)
		}
	}
}

// 全序断言：归并键先比 updated_at，再比 id（同毫秒不会漏页/重页）。
func TestBeforeKeyTotalOrder(t *testing.T) {
	a := time.Date(2026, 7, 12, 8, 0, 0, 0, time.UTC)
	b := a.Add(time.Millisecond)
	if !beforeKey(a, "zzz", b, "aaa") {
		t.Fatal("earlier updated_at must sort first regardless of id")
	}
	if !beforeKey(a, "aaa", a, "bbb") || beforeKey(a, "bbb", a, "aaa") {
		t.Fatal("same updated_at must tie-break by id ascending")
	}
}
