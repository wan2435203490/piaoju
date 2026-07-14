package importer

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// dupWindow 查重时间窗：同金额 + 同方向 + occurredAt 相差 ≤ 60s → 视为重复（契约 §6.2）。
// 账单时间与用户手记时间常有分钟内偏差，故不做精确相等。
const dupWindow = 60 * time.Second

// sqlSelectDupCandidates 查重候选：一次性把「账单时间范围 ± 窗口」内的交易全捞出来，
// 在内存里比对 —— 不允许每行一个 query（N+1）。
// 必带 user_id 条件（conventions §2：漏带即安全 bug）。
const sqlSelectDupCandidates = "SELECT amount_cents, direction, occurred_at FROM transactions" +
	" WHERE user_id = ? AND deleted_at IS NULL AND occurred_at >= ? AND occurred_at <= ?"

type service struct {
	db *sql.DB
}

// ImportRow 契约 §6.2 ImportRow。categoryId 为规则匹配的建议值，duplicate=true 建议客户端跳过。
type ImportRow struct {
	RowIndex      int    `json:"rowIndex"`
	AmountCents   int64  `json:"amountCents"`
	Direction     string `json:"direction"`
	OccurredAt    string `json:"occurredAt"`
	Note          string `json:"note"`
	PaymentMethod string `json:"paymentMethod"`
	CategoryID    int64  `json:"categoryId"`
	Duplicate     bool   `json:"duplicate"`
}

// previewResult 契约 §6.2 响应 data。
type previewResult struct {
	Items      []ImportRow `json:"items"`
	Total      int         `json:"total"`
	Duplicates int         `json:"duplicates"`
}

// existing 库中一笔已有交易的查重键。
type existing struct {
	amountCents int64
	direction   string
	occurredAt  time.Time
}

// preview 解析 → 归一化 → 规则分类 → 查重。不落库：写入由客户端走 §8 sync/push
// （离线安全、幂等、复用 LWW）。
func (s *service) preview(ctx context.Context, uid int64, source string, data []byte) (*previewResult, error) {
	rows, err := parse(source, data)
	if err != nil {
		return nil, err
	}

	dups, err := s.markDuplicates(ctx, uid, rows)
	if err != nil {
		return nil, err
	}

	items := make([]ImportRow, 0, len(rows))
	for i, r := range rows {
		items = append(items, ImportRow{
			RowIndex:      r.RowIndex,
			AmountCents:   r.AmountCents,
			Direction:     r.Direction,
			OccurredAt:    r.OccurredAt.UTC().Format(time.RFC3339),
			Note:          r.Note,
			PaymentMethod: r.PaymentMethod,
			CategoryID:    r.CategoryID,
			Duplicate:     dups[i],
		})
	}

	n := 0
	for _, d := range dups {
		if d {
			n++
		}
	}
	return &previewResult{Items: items, Total: len(items), Duplicates: n}, nil
}

// markDuplicates 批量查重：一次 query 捞出时间范围内的候选，内存里 O(n*m) 比对
// （单次导入行数与窗口内交易数都有限，无需再建索引结构）。
// 判重条件：同 user_id + 同 amountCents + 同 direction + occurredAt 在 ±60s 内。
func (s *service) markDuplicates(ctx context.Context, uid int64, rows []parsedRow) ([]bool, error) {
	dups := make([]bool, len(rows))
	if len(rows) == 0 {
		return dups, nil
	}

	lo, hi := rows[0].OccurredAt, rows[0].OccurredAt
	for _, r := range rows[1:] {
		if r.OccurredAt.Before(lo) {
			lo = r.OccurredAt
		}
		if r.OccurredAt.After(hi) {
			hi = r.OccurredAt
		}
	}

	sqlRows, err := s.db.QueryContext(ctx, sqlSelectDupCandidates,
		uid, lo.Add(-dupWindow).UTC(), hi.Add(dupWindow).UTC())
	if err != nil {
		return nil, fmt.Errorf("importer: dup candidates query: %w", err)
	}
	defer sqlRows.Close()

	var candidates []existing
	for sqlRows.Next() {
		var e existing
		if err := sqlRows.Scan(&e.amountCents, &e.direction, &e.occurredAt); err != nil {
			return nil, fmt.Errorf("importer: dup candidates scan: %w", err)
		}
		candidates = append(candidates, e)
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("importer: dup candidates rows: %w", err)
	}

	for i, r := range rows {
		for _, c := range candidates {
			if c.amountCents != r.AmountCents || c.direction != r.Direction {
				continue
			}
			if absDur(c.occurredAt.Sub(r.OccurredAt)) <= dupWindow {
				dups[i] = true
				break
			}
		}
	}
	return dups, nil
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
