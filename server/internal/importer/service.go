package importer

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// maxDupCandidates 查重候选行数硬上限（DoS 防御）。
// 时间区间由用户 CSV 的最小/最大 occurredAt 决定，攻击者可放两行（如 1970 与 2099）
// 把区间撑成「整张表」，若不封顶就会把该用户全部交易读进内存。查重是尽力而为的建议，
// 漏判几笔不影响正确性（客户端仍可手动去重），故直接 LIMIT 封顶。
const maxDupCandidates = 100000

// sqlSelectDupCandidates 查重候选：一次性把账单时间范围内的交易捞出来，
// 在内存里按 amountCents 建索引、精确比对 occurredAt —— 不允许每行一个 query（N+1）。
// 契约 §6.2：duplicate = 与库中「同金额 + 同时刻」的交易撞上（不看方向、不设时间窗）。
// 必带 user_id 条件（conventions §2：漏带即安全 bug）；LIMIT 封顶候选量（见 maxDupCandidates）。
const sqlSelectDupCandidates = "SELECT amount_cents, occurred_at FROM transactions" +
	" WHERE user_id = ? AND deleted_at IS NULL AND occurred_at >= ? AND occurred_at <= ? LIMIT ?"

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

// markDuplicates 批量查重：一次 query 捞出账单时间范围内的候选（LIMIT 封顶），
// 按 amountCents 建索引，逐行判断是否存在「同金额 + 同时刻」的已有交易（契约 §6.2）→ O(n+m)。
// 判重条件：同 user_id + 同 amountCents + occurredAt 精确相等（不看 direction、不设时间窗）。
//
// 安全：时间区间由用户 CSV 决定，故 SQL 带 LIMIT 防全表读入；比对用哈希集合而非裸双重循环，
// 防攻击者构造同额的 n 行 × m 候选把 CPU 打满；长循环周期性检查 ctx，客户端断开即停。
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
		uid, lo.UTC(), hi.UTC(), maxDupCandidates)
	if err != nil {
		return nil, fmt.Errorf("importer: dup candidates query: %w", err)
	}
	defer sqlRows.Close()

	// index[amountCents] = 该金额下所有已有交易的 occurredAt（毫秒）集合，供「同时刻」精确命中。
	index := make(map[int64]map[int64]struct{})
	for sqlRows.Next() {
		var amountCents int64
		var occurredAt time.Time
		if err := sqlRows.Scan(&amountCents, &occurredAt); err != nil {
			return nil, fmt.Errorf("importer: dup candidates scan: %w", err)
		}
		set := index[amountCents]
		if set == nil {
			set = make(map[int64]struct{})
			index[amountCents] = set
		}
		set[occurredAt.UnixMilli()] = struct{}{}
	}
	if err := sqlRows.Err(); err != nil {
		return nil, fmt.Errorf("importer: dup candidates rows: %w", err)
	}

	for i, r := range rows {
		if i&1023 == 0 { // 客户端断开 / 超时后不再空转 CPU
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if set := index[r.AmountCents]; set != nil {
			if _, ok := set[r.OccurredAt.UnixMilli()]; ok {
				dups[i] = true
			}
		}
	}
	return dups, nil
}
