package category

// service 层单测：与 transaction 模块同约束（本机无 Docker/MySQL），用 go-sqlmock
// 精确断言 SQL + 参数（regexp.QuoteMeta 复用 service.go 的 SQL 常量）。
// 真库路径由 Wave 3 冒烟覆盖。

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"regexp"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"

	"piaoju/internal/platform/apperr"
)

const uidA int64 = 7

var fixedNow = time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)

func newSvc(t *testing.T) (*service, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &service{db: db, now: func() time.Time { return fixedNow }}, mock
}

func code(t *testing.T, err error) int {
	t.Helper()
	ae, ok := err.(*apperr.Error)
	if !ok {
		t.Fatalf("err = %v, want *apperr.Error", err)
	}
	return ae.Code
}

func catRows(rows ...[]driverValue) *sqlmock.Rows {
	r := sqlmock.NewRows([]string{"id", "user_id", "name", "icon", "kind", "sort"})
	for _, v := range rows {
		r.AddRow(v...)
	}
	return r
}

type driverValue = driver.Value

func TestListIncludesSystemAndOwn(t *testing.T) {
	svc, mock := newSvc(t)
	mock.ExpectQuery(regexp.QuoteMeta(sqlListCategories)).WithArgs(uidA).
		WillReturnRows(catRows(
			[]driverValue{int64(1), nil, "餐饮", "🍜", "expense", 1},
			[]driverValue{int64(12), uidA, "宠物", "🐱", "expense", 9},
		))
	items, err := svc.list(context.Background(), uidA)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("len = %d, want 2", len(items))
	}
	if !items[0].IsSystem || items[1].IsSystem {
		t.Fatalf("isSystem flags wrong: %+v", items)
	}
}

func TestCreateAppendsSort(t *testing.T) {
	svc, mock := newSvc(t)
	mock.ExpectQuery(regexp.QuoteMeta(sqlNextSort)).WithArgs(uidA, "expense").
		WillReturnRows(sqlmock.NewRows([]string{"s"}).AddRow(9))
	mock.ExpectExec(regexp.QuoteMeta(sqlInsertCategory)).
		WithArgs(uidA, "宠物", "🐱", "expense", 9).
		WillReturnResult(sqlmock.NewResult(12, 1))
	c, err := svc.create(context.Background(), uidA, input{Name: "宠物", Icon: "🐱", Kind: "expense"})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != 12 || c.Sort != 9 || c.IsSystem {
		t.Fatalf("category = %+v", c)
	}
}

func TestPatchSystemOrOthersRowIs40401(t *testing.T) {
	svc, mock := newSvc(t)
	// 系统分类 user_id IS NULL、他人分类 user_id != uid，sqlGetOwn 均查不到行。
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetOwn)).WithArgs(int64(1), uidA).
		WillReturnError(sql.ErrNoRows)
	name := "改名"
	_, err := svc.patch(context.Background(), uidA, 1, patchInput{Name: &name})
	if got := code(t, err); got != apperr.CodeNotFound {
		t.Fatalf("code = %d, want %d", got, apperr.CodeNotFound)
	}
}

func TestPatchOwnPartial(t *testing.T) {
	svc, mock := newSvc(t)
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetOwn)).WithArgs(int64(12), uidA).
		WillReturnRows(catRows([]driverValue{int64(12), uidA, "宠物", "🐱", "expense", 9}))
	mock.ExpectExec(regexp.QuoteMeta(sqlUpdateCategory)).
		WithArgs("毛孩子", "🐱", 9, int64(12), uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	name := "毛孩子"
	c, err := svc.patch(context.Background(), uidA, 12, patchInput{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "毛孩子" || c.Icon != "🐱" {
		t.Fatalf("category = %+v", c)
	}
}

func TestRemoveReassignsTxToOther(t *testing.T) {
	svc, mock := newSvc(t)
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetOwn)).WithArgs(int64(12), uidA).
		WillReturnRows(catRows([]driverValue{int64(12), uidA, "宠物", "🐱", "expense", 9}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDelCategory)).
		WithArgs(fixedNow, int64(12), uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlReassignTx)).
		WithArgs(otherExpenseID, fixedNow, uidA, int64(12)).
		WillReturnResult(sqlmock.NewResult(0, 3))
	mock.ExpectCommit()
	if err := svc.remove(context.Background(), uidA, 12); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveIncomeGoesToIncomeOther(t *testing.T) {
	svc, mock := newSvc(t)
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetOwn)).WithArgs(int64(13), uidA).
		WillReturnRows(catRows([]driverValue{int64(13), uidA, "外快", "💵", "income", 4}))
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(sqlSoftDelCategory)).
		WithArgs(fixedNow, int64(13), uidA).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(sqlReassignTx)).
		WithArgs(otherIncomeID, fixedNow, uidA, int64(13)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	if err := svc.remove(context.Background(), uidA, 13); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveNotFound(t *testing.T) {
	svc, mock := newSvc(t)
	mock.ExpectQuery(regexp.QuoteMeta(sqlGetOwn)).WithArgs(int64(99), uidA).
		WillReturnError(sql.ErrNoRows)
	err := svc.remove(context.Background(), uidA, 99)
	if got := code(t, err); got != apperr.CodeNotFound {
		t.Fatalf("code = %d, want %d", got, apperr.CodeNotFound)
	}
}
