// Package stats 契约 §7：月度收支统计与票夹年度统计。
//
// 全部为纯 SQL 聚合（GROUP BY/SUM/COUNT），不把明细集加载进内存（任务卡硬性要求）；
// 软删行（deleted_at 非空）一律排除。
//
// 契约 §7 要求对 transactions / tickets 两表做只读聚合——契约对「模块不互查对方表」
// 禁令的显式豁免；本模块对这两张表只读，不做任何写入。
package stats

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SQL 常量（测试用 regexp.QuoteMeta 复用，防实现/断言漂移）。
// 所有语句一律带 user_id 条件（conventions §2：漏带即安全 bug）。
const (
	// 当月两向总额（expense/income 各一行；无数据的方向缺行，Go 侧补 0）。
	sqlMonthlyTotals = "SELECT direction, SUM(amount_cents) FROM transactions" +
		" WHERE user_id = ? AND deleted_at IS NULL AND occurred_at >= ? AND occurred_at < ?" +
		" GROUP BY direction"

	// byCategory 仅统计 expense（契约 §7 v1.1 口径）；金额降序、同额按分类 id 升序（图表友好且确定性）。
	sqlMonthlyByCategory = "SELECT category_id, SUM(amount_cents) AS cents, COUNT(*) FROM transactions" +
		" WHERE user_id = ? AND direction = 'expense' AND deleted_at IS NULL AND occurred_at >= ? AND occurred_at < ?" +
		" GROUP BY category_id ORDER BY cents DESC, category_id"

	// byDay 仅统计 expense；只含有支出的日期（无支出的日期缺行，前端自行补零）。
	// 日边界按 UTC 计（conventions §1：DB 存 UTC，连接强制 loc=UTC）。
	sqlMonthlyByDay = "SELECT DATE(occurred_at) AS d, SUM(amount_cents) FROM transactions" +
		" WHERE user_id = ? AND direction = 'expense' AND deleted_at IS NULL AND occurred_at >= ? AND occurred_at < ?" +
		" GROUP BY d ORDER BY d"

	// 票夹年度统计：kind 分桶计数 + 关联交易金额合计；ENUM 排序即 kind 定义序
	// （movie/show/attraction/train/flight/other，对齐前端 fixtures 呈现序）。
	// tickets.transaction_id NOT NULL 且软删联动（票删则交易同删），INNER JOIN 安全。
	sqlTicketsByKind = "SELECT t.kind, COUNT(*), SUM(tx.amount_cents) FROM tickets t" +
		" JOIN transactions tx ON tx.id = t.transaction_id" +
		" WHERE t.user_id = ? AND t.deleted_at IS NULL AND t.event_time >= ? AND t.event_time < ?" +
		" GROUP BY t.kind ORDER BY t.kind"
)

type service struct {
	db *sql.DB
}

// CategoryStat 契约 §7 byCategory 元素（expense only）。
type CategoryStat struct {
	CategoryID int64 `json:"categoryId"`
	Cents      int64 `json:"cents"`
	Count      int64 `json:"count"`
}

// DayStat 契约 §7 byDay 元素（expense only），date 形如 "2026-07-01"。
type DayStat struct {
	Date         string `json:"date"`
	ExpenseCents int64  `json:"expenseCents"`
}

// MonthlyStats GET /stats/monthly 响应 data。
type MonthlyStats struct {
	ExpenseCents int64          `json:"expenseCents"`
	IncomeCents  int64          `json:"incomeCents"`
	ByCategory   []CategoryStat `json:"byCategory"`
	ByDay        []DayStat      `json:"byDay"`
}

// KindStat 契约 §7 byKind 元素；cents 来自票关联交易的金额合计。
type KindStat struct {
	Kind  string `json:"kind"`
	Count int64  `json:"count"`
	Cents int64  `json:"cents"`
}

// TicketStats GET /stats/tickets 响应 data。
type TicketStats struct {
	Total  int64      `json:"total"`
	ByKind []KindStat `json:"byKind"`
}

// monthly 月度统计：三条聚合查询（总额/分类/逐日），[start, end) 为 UTC 月边界。
func (s *service) monthly(ctx context.Context, uid int64, start, end time.Time) (*MonthlyStats, error) {
	out := &MonthlyStats{
		ByCategory: make([]CategoryStat, 0),
		ByDay:      make([]DayStat, 0),
	}

	rows, err := s.db.QueryContext(ctx, sqlMonthlyTotals, uid, start, end)
	if err != nil {
		return nil, fmt.Errorf("stats: monthly totals query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var direction string
		var cents int64
		if err := rows.Scan(&direction, &cents); err != nil {
			return nil, fmt.Errorf("stats: monthly totals scan: %w", err)
		}
		switch direction {
		case "expense":
			out.ExpenseCents = cents
		case "income":
			out.IncomeCents = cents
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats: monthly totals rows: %w", err)
	}

	catRows, err := s.db.QueryContext(ctx, sqlMonthlyByCategory, uid, start, end)
	if err != nil {
		return nil, fmt.Errorf("stats: monthly by-category query: %w", err)
	}
	defer catRows.Close()
	for catRows.Next() {
		var c CategoryStat
		if err := catRows.Scan(&c.CategoryID, &c.Cents, &c.Count); err != nil {
			return nil, fmt.Errorf("stats: monthly by-category scan: %w", err)
		}
		out.ByCategory = append(out.ByCategory, c)
	}
	if err := catRows.Err(); err != nil {
		return nil, fmt.Errorf("stats: monthly by-category rows: %w", err)
	}

	dayRows, err := s.db.QueryContext(ctx, sqlMonthlyByDay, uid, start, end)
	if err != nil {
		return nil, fmt.Errorf("stats: monthly by-day query: %w", err)
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var day time.Time // DATE(...) 经 parseTime=true 解析为 time.Time
		var cents int64
		if err := dayRows.Scan(&day, &cents); err != nil {
			return nil, fmt.Errorf("stats: monthly by-day scan: %w", err)
		}
		out.ByDay = append(out.ByDay, DayStat{Date: day.UTC().Format("2006-01-02"), ExpenseCents: cents})
	}
	if err := dayRows.Err(); err != nil {
		return nil, fmt.Errorf("stats: monthly by-day rows: %w", err)
	}
	return out, nil
}

// tickets 年度票夹统计：kind 分桶一条聚合查询，total = 各桶计数之和（最多 6 行，非明细集）。
// 年边界 [start, end) 按票的 event_time（UTC），与 GET /tickets?year= 过滤口径一致。
func (s *service) tickets(ctx context.Context, uid int64, start, end time.Time) (*TicketStats, error) {
	out := &TicketStats{ByKind: make([]KindStat, 0)}

	rows, err := s.db.QueryContext(ctx, sqlTicketsByKind, uid, start, end)
	if err != nil {
		return nil, fmt.Errorf("stats: tickets by-kind query: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var k KindStat
		if err := rows.Scan(&k.Kind, &k.Count, &k.Cents); err != nil {
			return nil, fmt.Errorf("stats: tickets by-kind scan: %w", err)
		}
		out.ByKind = append(out.ByKind, k)
		out.Total += k.Count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stats: tickets by-kind rows: %w", err)
	}
	return out, nil
}
