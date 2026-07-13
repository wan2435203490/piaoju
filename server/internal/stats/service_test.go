package stats

// service 层单测：本机无 Docker/MySQL（同 platform/db 约束），用 go-sqlmock 精确
// 断言 SQL 语句 + 参数（regexp.QuoteMeta 复用 service.go 的 SQL 常量），并把聚合
// 结果与手算 fixture 对账。真库 GROUP BY 执行路径由 Wave 3 docker compose 冒烟覆盖。

import (
	"context"
	"reflect"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
)

const uidA int64 = 7

var (
	julyStart = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	julyEnd   = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	yearStart = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	yearEnd   = time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)
)

func newTestService(t *testing.T) (*service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return &service{db: db}, mock
}

func mustMeet(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("unmet expectations: %v", err)
	}
}

// TestMonthlyHandComputed 手算对账。原始流水 fixture（uid=7，2026-07，全部未软删）：
//
//	07-01  expense  1200  cat1（早饭）
//	07-01  expense  3800  cat2（奶茶）
//	07-03  expense  2500  cat1（午饭）
//	07-15  income 500000  cat11（工资）
//
// 手算：expenseCents = 1200+3800+2500 = 7500；incomeCents = 500000。
// byCategory（仅 expense，cents 降序）：cat2 3800×1、cat1 (1200+2500)=3700×2。
// byDay（仅 expense）：07-01 = 1200+3800 = 5000；07-03 = 2500。
// mock 返回的即上述流水经 GROUP BY 后的行，响应必须与手算逐项一致。
func TestMonthlyHandComputed(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyTotals)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "cents"}).
			AddRow("expense", 7500).
			AddRow("income", 500000))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByCategory)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"category_id", "cents", "count"}).
			AddRow(2, 3800, 1).
			AddRow(1, 3700, 2))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByDay)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"d", "cents"}).
			AddRow(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 5000).
			AddRow(time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), 2500))

	got, err := svc.monthly(context.Background(), uidA, julyStart, julyEnd)
	if err != nil {
		t.Fatalf("monthly: %v", err)
	}
	want := &MonthlyStats{
		ExpenseCents: 7500,
		IncomeCents:  500000,
		ByCategory: []CategoryStat{
			{CategoryID: 2, Cents: 3800, Count: 1},
			{CategoryID: 1, Cents: 3700, Count: 2},
		},
		ByDay: []DayStat{
			{Date: "2026-07-01", ExpenseCents: 5000},
			{Date: "2026-07-03", ExpenseCents: 2500},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("monthly = %+v, want %+v", got, want)
	}
	mustMeet(t, mock)
}

// TestMonthlyExpenseOnlyMonth 只有支出、无收入 → incomeCents 补 0（GROUP BY 缺行不 panic）。
func TestMonthlyExpenseOnlyMonth(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyTotals)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "cents"}).AddRow("expense", 1200))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByCategory)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"category_id", "cents", "count"}).AddRow(1, 1200, 1))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByDay)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"d", "cents"}).
			AddRow(time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), 1200))

	got, err := svc.monthly(context.Background(), uidA, julyStart, julyEnd)
	if err != nil {
		t.Fatalf("monthly: %v", err)
	}
	if got.ExpenseCents != 1200 || got.IncomeCents != 0 {
		t.Fatalf("totals = %d/%d, want 1200/0", got.ExpenseCents, got.IncomeCents)
	}
	mustMeet(t, mock)
}

// TestMonthlyEmptyMonth 空月 → 全 0 + 空数组（非 null）。
func TestMonthlyEmptyMonth(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyTotals)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "cents"}))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByCategory)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"category_id", "cents", "count"}))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByDay)).
		WithArgs(uidA, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"d", "cents"}))

	got, err := svc.monthly(context.Background(), uidA, julyStart, julyEnd)
	if err != nil {
		t.Fatalf("monthly: %v", err)
	}
	if got.ExpenseCents != 0 || got.IncomeCents != 0 {
		t.Fatalf("totals = %+v, want zeros", got)
	}
	if got.ByCategory == nil || len(got.ByCategory) != 0 || got.ByDay == nil || len(got.ByDay) != 0 {
		t.Fatalf("byCategory/byDay = %v/%v, want empty non-nil slices", got.ByCategory, got.ByDay)
	}
	mustMeet(t, mock)
}

// TestTicketsHandComputed 手算对账（数字对齐 web fixtures stats-tickets.json）：
// 五张票各 kind 一张，cents 为各票关联交易金额合计；total = 各桶 count 之和 = 5。
func TestTicketsHandComputed(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectQuery(regexp.QuoteMeta(sqlTicketsByKind)).
		WithArgs(uidA, yearStart, yearEnd).
		WillReturnRows(sqlmock.NewRows([]string{"kind", "count", "cents"}).
			AddRow("movie", 1, 9900).
			AddRow("show", 1, 28000).
			AddRow("attraction", 1, 4500).
			AddRow("train", 1, 62300).
			AddRow("flight", 1, 128000))

	got, err := svc.tickets(context.Background(), uidA, yearStart, yearEnd)
	if err != nil {
		t.Fatalf("tickets: %v", err)
	}
	want := &TicketStats{
		Total: 5,
		ByKind: []KindStat{
			{Kind: "movie", Count: 1, Cents: 9900},
			{Kind: "show", Count: 1, Cents: 28000},
			{Kind: "attraction", Count: 1, Cents: 4500},
			{Kind: "train", Count: 1, Cents: 62300},
			{Kind: "flight", Count: 1, Cents: 128000},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tickets = %+v, want %+v", got, want)
	}
	mustMeet(t, mock)
}

// TestTicketsEmptyYear 空年 → total=0、byKind=[]（非 null）。
func TestTicketsEmptyYear(t *testing.T) {
	svc, mock := newTestService(t)

	mock.ExpectQuery(regexp.QuoteMeta(sqlTicketsByKind)).
		WithArgs(uidA, yearStart, yearEnd).
		WillReturnRows(sqlmock.NewRows([]string{"kind", "count", "cents"}))

	got, err := svc.tickets(context.Background(), uidA, yearStart, yearEnd)
	if err != nil {
		t.Fatalf("tickets: %v", err)
	}
	if got.Total != 0 || got.ByKind == nil || len(got.ByKind) != 0 {
		t.Fatalf("tickets = %+v, want total 0 with empty non-nil byKind", got)
	}
	mustMeet(t, mock)
}

// TestUserIsolationArgs 换个 uid 调用：所有聚合查询的第一个参数必须是该 uid
// （SQL 常量本身带 user_id = ? 条件，sqlmock WithArgs 强校验传参）。
func TestUserIsolationArgs(t *testing.T) {
	svc, mock := newTestService(t)
	const uidB int64 = 42

	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyTotals)).
		WithArgs(uidB, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"direction", "cents"}))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByCategory)).
		WithArgs(uidB, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"category_id", "cents", "count"}))
	mock.ExpectQuery(regexp.QuoteMeta(sqlMonthlyByDay)).
		WithArgs(uidB, julyStart, julyEnd).
		WillReturnRows(sqlmock.NewRows([]string{"d", "cents"}))

	if _, err := svc.monthly(context.Background(), uidB, julyStart, julyEnd); err != nil {
		t.Fatalf("monthly: %v", err)
	}
	mustMeet(t, mock)
}
