/**
 * 年度报告纯逻辑（W6 / 所有权目录 components/year）。
 *
 * 这里只做「数据 → 视图模型」的纯函数：年份选项、票型概览、地点提取、
 * 月度序列、年度之最。不碰 DOM、不碰网络（页面用 $lib/data 取数后喂进来），
 * 因此可以直接被 vitest 覆盖。
 *
 * 口径约定（conventions §1）：
 * - 金额一律整数「分」，渲染层才除 100
 * - eventTime / occurredAt 为 RFC3339 UTC；年份筛选沿用读层的 UTC 年（db/read.ts）
 */
import type { Category, CategoryStat, MonthlyStats, Ticket, TicketKind, TicketStats } from '$lib/api/types';
import { TICKET_KINDS } from '$lib/api/types';

/* ============ 年份 ============ */

/** 可选年份：当前年 + 往前 5 年（新 → 旧） */
export function yearOptions(currentYear: number, span = 6): number[] {
	return Array.from({ length: span }, (_, i) => currentYear - i);
}

/** ?year= 解析：非法/越界回退当前年 */
export function parseYear(raw: string | null | undefined, currentYear: number, span = 6): number {
	const n = Number(raw);
	if (!Number.isInteger(n)) return currentYear;
	return yearOptions(currentYear, span).includes(n) ? n : currentYear;
}

/** 该年 12 个月的 "YYYY-MM" */
export function monthKeys(year: number): string[] {
	return Array.from({ length: 12 }, (_, i) => `${year}-${String(i + 1).padStart(2, '0')}`);
}

/* ============ 票型概览 ============ */

export interface KindCount {
	kind: TicketKind;
	count: number;
	cents: number;
}

export interface KindSummary {
	total: number;
	totalCents: number;
	/** 固定票型顺序（色标跟实体走，不随排名重涂 —— dataviz） */
	byKind: KindCount[];
}

/**
 * 票型分布。stats（契约 §7）为准；stats 缺失（接口失败）时用票根列表兜底聚合，
 * 保证概览卡不因为一个接口挂掉就整页空白。
 */
export function kindSummary(stats: TicketStats | null, tickets: Ticket[]): KindSummary {
	const map = new Map<TicketKind, { count: number; cents: number }>();

	if (stats) {
		for (const k of stats.byKind) map.set(k.kind, { count: k.count, cents: k.cents });
	} else {
		for (const t of tickets) {
			const cur = map.get(t.kind) ?? { count: 0, cents: 0 };
			cur.count += 1;
			cur.cents += t.transaction.amountCents;
			map.set(t.kind, cur);
		}
	}

	const byKind: KindCount[] = TICKET_KINDS.map((kind) => ({
		kind,
		count: map.get(kind)?.count ?? 0,
		cents: map.get(kind)?.cents ?? 0
	}));

	return {
		total: stats ? stats.total : byKind.reduce((acc, k) => acc + k.count, 0),
		totalCents: byKind.reduce((acc, k) => acc + k.cents, 0),
		byKind
	};
}

/* ============ 地点足迹 ============ */

export interface PlaceVisit {
	name: string;
	count: number;
	/** 该地点出现过的票型（固定票型顺序，用于色标） */
	kinds: TicketKind[];
}

/**
 * 站名/机场名归一到「城市」粒度：
 *   北京南站 → 北京   上海虹桥站 → 上海虹桥   广州白云国际机场 → 广州白云
 * 只做保守的后缀剥离，剥完为空则退回原文（宁可多一个条目，也不要把地名改错）。
 */
export function normalizePlace(raw: string): string {
	const t = raw.trim();
	if (!t) return '';
	let s = t.replace(/(国际机场|机场|火车站|站)$/u, '');
	// 方位后缀：仅在剥离后仍 ≥ 2 字时去掉（北京南 → 北京；不动「浦东」这类）
	if (s.length >= 3 && /[东南西北]$/u.test(s)) s = s.slice(0, -1);
	return s || t;
}

/**
 * 从票根提取去过的地点：
 * attraction.city / train.fromStation+toStation / flight.fromAirport+toAirport。
 * 同一张票内的重复地点只计一次（往返同城不重复计数）。
 */
export function extractPlaces(tickets: Ticket[]): PlaceVisit[] {
	const map = new Map<string, { count: number; kinds: Set<TicketKind> }>();

	for (const t of tickets) {
		const raw: string[] = [];
		if (t.kind === 'attraction') {
			const e = t.extra as { city?: string };
			raw.push(e.city ?? '');
		} else if (t.kind === 'train') {
			const e = t.extra as { fromStation?: string; toStation?: string };
			raw.push(e.fromStation ?? '', e.toStation ?? '');
		} else if (t.kind === 'flight') {
			const e = t.extra as { fromAirport?: string; toAirport?: string };
			raw.push(e.fromAirport ?? '', e.toAirport ?? '');
		} else {
			continue;
		}

		const names = new Set(raw.map(normalizePlace).filter((n) => n !== ''));
		for (const name of names) {
			const cur = map.get(name) ?? { count: 0, kinds: new Set<TicketKind>() };
			cur.count += 1;
			cur.kinds.add(t.kind);
			map.set(name, cur);
		}
	}

	return [...map]
		.map(([name, v]) => ({
			name,
			count: v.count,
			kinds: TICKET_KINDS.filter((k) => v.kinds.has(k))
		}))
		.sort((a, b) => (b.count !== a.count ? b.count - a.count : a.name.localeCompare(b.name, 'zh')));
}

/* ============ 月度支出 ============ */

export interface MonthPoint {
	/** "YYYY-MM" */
	month: string;
	/** 1..12 */
	index: number;
	cents: number;
}

/**
 * 12 个月支出序列。`monthly[i]` 为该月统计，失败月传 null → 降级成 0
 * （单月接口挂掉不该让整页崩）。
 */
export function monthlySeries(year: number, monthly: (MonthlyStats | null)[]): MonthPoint[] {
	return monthKeys(year).map((month, i) => ({
		month,
		index: i + 1,
		cents: monthly[i]?.expenseCents ?? 0
	}));
}

/** 12 个月的 byCategory 合并成年度分类支出（降序） */
export function mergeCategoryStats(monthly: (MonthlyStats | null)[]): CategoryStat[] {
	const map = new Map<number, { cents: number; count: number }>();
	for (const m of monthly) {
		if (!m) continue;
		for (const c of m.byCategory) {
			const cur = map.get(c.categoryId) ?? { cents: 0, count: 0 };
			cur.cents += c.cents;
			cur.count += c.count;
			map.set(c.categoryId, cur);
		}
	}
	return [...map]
		.map(([categoryId, v]) => ({ categoryId, ...v }))
		.sort((a, b) => b.cents - a.cents);
}

/* ============ 年度之最 ============ */

export interface VenueCount {
	name: string;
	count: number;
}

export interface Superlatives {
	/** 最贵的一张票 */
	priciest: Ticket | null;
	/** 评分最高的票（rating = 0 视为未评分，不参与） */
	topRated: Ticket | null;
	/** 去得最多的场馆/影院（至少 2 次才算「最多」） */
	topVenue: VenueCount | null;
	/** 花钱最多的分类（来自月度统计合并） */
	topCategory: CategoryStat | null;
}

/** 票根归属的场馆：venue 优先，电影票退回影院名 */
export function ticketVenue(t: Ticket): string {
	if (t.venue.trim()) return t.venue.trim();
	if (t.kind === 'movie') return ((t.extra as { cinema?: string }).cinema ?? '').trim();
	return '';
}

export function superlatives(tickets: Ticket[], byCategory: CategoryStat[]): Superlatives {
	let priciest: Ticket | null = null;
	let topRated: Ticket | null = null;
	const venues = new Map<string, number>();

	for (const t of tickets) {
		if (!priciest || t.transaction.amountCents > priciest.transaction.amountCents) priciest = t;

		if (t.rating > 0) {
			// 同分取更贵的那张（更有「年度之最」意味），再同则取先出现的
			if (
				!topRated ||
				t.rating > topRated.rating ||
				(t.rating === topRated.rating &&
					t.transaction.amountCents > topRated.transaction.amountCents)
			) {
				topRated = t;
			}
		}

		const venue = ticketVenue(t);
		if (venue) venues.set(venue, (venues.get(venue) ?? 0) + 1);
	}

	let topVenue: VenueCount | null = null;
	for (const [name, count] of venues) {
		if (count < 2) continue;
		if (!topVenue || count > topVenue.count) topVenue = { name, count };
	}

	return { priciest, topRated, topVenue, topCategory: byCategory[0] ?? null };
}

/** 分类展示名（缺分类时退回 "分类 #id"，与统计页一致） */
export function categoryLabel(categories: Category[], id: number): string {
	const c = categories.find((x) => x.id === id);
	return c ? `${c.icon} ${c.name}` : `分类 #${id}`;
}
