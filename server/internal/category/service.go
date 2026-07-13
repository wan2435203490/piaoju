// Package category 契约 §3 分类 CRUD：系统预设（user_id NULL）+ 用户自定义。
// 仅自定义分类可改删；删除后其交易归入同向系统「其他」。
package category

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"piaoju/internal/platform/apperr"
)

// 系统「其他」分类 id（迁移 0002 固定 id 1-11，见 SQL 注释）。
const (
	otherExpenseID int64 = 8
	otherIncomeID  int64 = 11
)

// SQL 常量（测试用 regexp.QuoteMeta 复用，防实现/断言漂移）。
// 写语句一律带 user_id = ? 条件（conventions §2）；读含系统行（user_id IS NULL）。
const (
	sqlListCategories = "SELECT id, user_id, name, icon, kind, sort FROM categories" +
		" WHERE (user_id IS NULL OR user_id = ?) AND deleted_at IS NULL ORDER BY kind, sort, id"

	sqlNextSort = "SELECT COALESCE(MAX(sort), 0) + 1 FROM categories" +
		" WHERE (user_id IS NULL OR user_id = ?) AND kind = ? AND deleted_at IS NULL"

	sqlInsertCategory = "INSERT INTO categories (user_id, name, icon, kind, sort) VALUES (?, ?, ?, ?, ?)"

	sqlGetOwn = "SELECT id, user_id, name, icon, kind, sort FROM categories" +
		" WHERE id = ? AND user_id = ? AND deleted_at IS NULL"

	sqlUpdateCategory = "UPDATE categories SET name = ?, icon = ?, sort = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL"

	sqlSoftDelCategory = "UPDATE categories SET deleted_at = ? WHERE id = ? AND user_id = ? AND deleted_at IS NULL"

	// 契约 §3「删除后其交易归入『其他』」——对 transactions 的显式跨模块写豁免，
	// 仅此一句；bump updated_at 供 sync 墓碑/增量下发。
	sqlReassignTx = "UPDATE transactions SET category_id = ?, updated_at = ? WHERE user_id = ? AND category_id = ? AND deleted_at IS NULL"
)

// Category 契约 §3 响应形状（isSystem = user_id IS NULL）。
type Category struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	Kind     string `json:"kind"`
	IsSystem bool   `json:"isSystem"`
	Sort     int    `json:"sort"`
}

type service struct {
	db  *sql.DB
	now func() time.Time
}

func scanCategory(rows interface{ Scan(...any) error }) (Category, error) {
	var c Category
	var userID sql.NullInt64
	if err := rows.Scan(&c.ID, &userID, &c.Name, &c.Icon, &c.Kind, &c.Sort); err != nil {
		return c, err
	}
	c.IsSystem = !userID.Valid
	return c, nil
}

func (s *service) list(ctx context.Context, uid int64) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx, sqlListCategories, uid)
	if err != nil {
		return nil, internal(err)
	}
	defer rows.Close()
	items := make([]Category, 0, 16)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, internal(err)
		}
		items = append(items, c)
	}
	if err := rows.Err(); err != nil {
		return nil, internal(err)
	}
	return items, nil
}

func (s *service) create(ctx context.Context, uid int64, in input) (*Category, error) {
	var sort int
	if err := s.db.QueryRowContext(ctx, sqlNextSort, uid, in.Kind).Scan(&sort); err != nil {
		return nil, internal(err)
	}
	res, err := s.db.ExecContext(ctx, sqlInsertCategory, uid, in.Name, in.Icon, in.Kind, sort)
	if err != nil {
		return nil, internal(err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, internal(err)
	}
	return &Category{ID: id, Name: in.Name, Icon: in.Icon, Kind: in.Kind, Sort: sort}, nil
}

// patch 仅自定义分类（user_id = uid）；系统分类与他人分类同回 40401（契约 §1 不存在或无权访问）。
func (s *service) patch(ctx context.Context, uid, id int64, p patchInput) (*Category, error) {
	cur, err := scanCategory(s.db.QueryRowContext(ctx, sqlGetOwn, id, uid))
	if err == sql.ErrNoRows {
		return nil, apperr.New(apperr.CodeNotFound, "category not found")
	}
	if err != nil {
		return nil, internal(err)
	}
	if p.Name != nil {
		cur.Name = *p.Name
	}
	if p.Icon != nil {
		cur.Icon = *p.Icon
	}
	if p.Sort != nil {
		cur.Sort = *p.Sort
	}
	if _, err := s.db.ExecContext(ctx, sqlUpdateCategory, cur.Name, cur.Icon, cur.Sort, id, uid); err != nil {
		return nil, internal(err)
	}
	return &cur, nil
}

// remove 软删自定义分类，事务内将其交易归入同向系统「其他」。
func (s *service) remove(ctx context.Context, uid, id int64) error {
	cur, err := scanCategory(s.db.QueryRowContext(ctx, sqlGetOwn, id, uid))
	if err == sql.ErrNoRows {
		return apperr.New(apperr.CodeNotFound, "category not found")
	}
	if err != nil {
		return internal(err)
	}
	fallback := otherExpenseID
	if cur.Kind == "income" {
		fallback = otherIncomeID
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return internal(err)
	}
	defer tx.Rollback()

	now := s.now().UTC()
	if _, err := tx.ExecContext(ctx, sqlSoftDelCategory, now, id, uid); err != nil {
		return internal(err)
	}
	if _, err := tx.ExecContext(ctx, sqlReassignTx, fallback, now, uid, id); err != nil {
		return internal(err)
	}
	if err := tx.Commit(); err != nil {
		return internal(err)
	}
	return nil
}

// internal 包装底层 DB 错误：不转 apperr（其 Msg 会原样进响应信封），
// 交由 httpx.Err 统一记日志并回固定的 "internal server error"。
func internal(err error) error {
	return fmt.Errorf("category: %w", err)
}
