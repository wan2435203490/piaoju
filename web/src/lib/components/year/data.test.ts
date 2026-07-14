import { describe, expect, it } from 'vitest';
import type { MonthlyStats, Ticket, TicketExtra, TicketKind, TicketStats } from '$lib/api/types';
import {
	categoryLabel,
	extractPlaces,
	kindSummary,
	mergeCategoryStats,
	monthKeys,
	monthlySeries,
	normalizePlace,
	parseYear,
	superlatives,
	ticketVenue,
	yearOptions
} from './data';

/* ---- fixtures ---- */

function ticket(over: {
	id?: string;
	kind: TicketKind;
	extra?: Partial<TicketExtra> | Record<string, string>;
	amountCents?: number;
	rating?: number;
	venue?: string;
	title?: string;
}): Ticket {
	return {
		id: over.id ?? crypto.randomUUID(),
		kind: over.kind,
		title: over.title ?? '票',
		venue: over.venue ?? '',
		eventTime: '2026-03-02T11:30:00Z',
		seat: '',
		extra: (over.extra ?? {}) as Ticket['extra'],
		rating: over.rating ?? 0,
		memo: '',
		transaction: {
			id: 'tx',
			amountCents: over.amountCents ?? 1000,
			categoryId: 1,
			paymentMethod: 'wechat'
		},
		attachments: [],
		createdAt: '2026-03-02T11:30:00Z',
		updatedAt: '2026-03-02T11:30:00Z'
	} as Ticket;
}

const monthly = (expenseCents: number, byCategory: MonthlyStats['byCategory'] = []): MonthlyStats => ({
	expenseCents,
	incomeCents: 0,
	byCategory,
	byDay: []
});

/* ---- 年份 ---- */

describe('yearOptions / parseYear / monthKeys', () => {
	it('当前年 + 往前 5 年，新到旧', () => {
		expect(yearOptions(2026)).toEqual([2026, 2025, 2024, 2023, 2022, 2021]);
	});

	it('parseYear 回退当前年', () => {
		expect(parseYear('2024', 2026)).toBe(2024);
		expect(parseYear(null, 2026)).toBe(2026);
		expect(parseYear('abc', 2026)).toBe(2026);
		expect(parseYear('1999', 2026)).toBe(2026); // 越界
		expect(parseYear('2027', 2026)).toBe(2026); // 未来
	});

	it('monthKeys 补零 12 个月', () => {
		const keys = monthKeys(2026);
		expect(keys).toHaveLength(12);
		expect(keys[0]).toBe('2026-01');
		expect(keys[11]).toBe('2026-12');
	});
});

/* ---- 票型概览 ---- */

describe('kindSummary', () => {
	it('以 stats 为准，补齐 6 种票型且顺序固定', () => {
		const stats: TicketStats = {
			total: 3,
			byKind: [
				{ kind: 'movie', count: 2, cents: 9000 },
				{ kind: 'train', count: 1, cents: 55300 }
			]
		};
		const s = kindSummary(stats, []);
		expect(s.total).toBe(3);
		expect(s.totalCents).toBe(64300);
		expect(s.byKind.map((k) => k.kind)).toEqual([
			'movie',
			'show',
			'attraction',
			'train',
			'flight',
			'other'
		]);
		expect(s.byKind.find((k) => k.kind === 'show')).toEqual({ kind: 'show', count: 0, cents: 0 });
	});

	it('stats 缺失时用票根兜底聚合', () => {
		const s = kindSummary(null, [
			ticket({ kind: 'movie', amountCents: 4500 }),
			ticket({ kind: 'movie', amountCents: 5500 }),
			ticket({ kind: 'flight', amountCents: 120000 })
		]);
		expect(s.total).toBe(3);
		expect(s.totalCents).toBe(130000);
		expect(s.byKind.find((k) => k.kind === 'movie')?.count).toBe(2);
	});
});

/* ---- 地点 ---- */

describe('normalizePlace', () => {
	it('剥离站/机场后缀与方位字', () => {
		expect(normalizePlace('北京南站')).toBe('北京');
		expect(normalizePlace('杭州东站')).toBe('杭州');
		expect(normalizePlace('上海虹桥站')).toBe('上海虹桥');
		expect(normalizePlace('广州白云国际机场')).toBe('广州白云');
		expect(normalizePlace('  成都  ')).toBe('成都');
	});

	it('剥完为空则退回原文，不改错地名', () => {
		expect(normalizePlace('站')).toBe('站');
		expect(normalizePlace('')).toBe('');
	});
});

describe('extractPlaces', () => {
	it('取门票城市 / 火车两端 / 航班两端，按次数降序', () => {
		const places = extractPlaces([
			ticket({ kind: 'train', extra: { fromStation: '北京南站', toStation: '上海虹桥站' } }),
			ticket({ kind: 'train', extra: { fromStation: '上海虹桥站', toStation: '北京南站' } }),
			ticket({ kind: 'attraction', extra: { city: '北京' } }),
			ticket({ kind: 'flight', extra: { fromAirport: '成都天府国际机场', toAirport: '北京大兴国际机场' } }),
			ticket({ kind: 'movie', extra: { cinema: '万达影城' } })
		]);
		expect(places[0]).toEqual({ name: '北京', count: 3, kinds: ['attraction', 'train'] });
		expect(places.map((p) => p.name)).toContain('上海虹桥');
		expect(places.map((p) => p.name)).toContain('成都天府');
		expect(places.map((p) => p.name)).toContain('北京大兴');
		// 电影票不产生地点
		expect(places.some((p) => p.name.includes('万达'))).toBe(false);
	});

	it('同一张票内往返同城只计一次', () => {
		const places = extractPlaces([
			ticket({ kind: 'train', extra: { fromStation: '北京南站', toStation: '北京西站' } })
		]);
		expect(places).toEqual([{ name: '北京', count: 1, kinds: ['train'] }]);
	});

	it('空字段不产生地点', () => {
		expect(extractPlaces([ticket({ kind: 'attraction', extra: { city: '  ' } })])).toEqual([]);
	});
});

/* ---- 月度 ---- */

describe('monthlySeries / mergeCategoryStats', () => {
	it('失败月降级成 0', () => {
		const series = monthlySeries(2026, [
			monthly(1000),
			null,
			monthly(2500),
			...Array(9).fill(null)
		]);
		expect(series).toHaveLength(12);
		expect(series[0]).toEqual({ month: '2026-01', index: 1, cents: 1000 });
		expect(series[1]?.cents).toBe(0);
		expect(series[2]?.cents).toBe(2500);
	});

	it('合并 12 个月的分类支出并降序', () => {
		const merged = mergeCategoryStats([
			monthly(0, [
				{ categoryId: 1, cents: 100, count: 1 },
				{ categoryId: 2, cents: 900, count: 2 }
			]),
			null,
			monthly(0, [{ categoryId: 1, cents: 300, count: 3 }])
		]);
		expect(merged).toEqual([
			{ categoryId: 2, cents: 900, count: 2 },
			{ categoryId: 1, cents: 400, count: 4 }
		]);
	});
});

/* ---- 年度之最 ---- */

describe('superlatives', () => {
	it('最贵 / 最高分 / 最常去场馆 / 最花钱分类', () => {
		const cheap = ticket({ id: 'a', kind: 'movie', amountCents: 4500, rating: 5, venue: '万达影城' });
		const pricey = ticket({ id: 'b', kind: 'flight', amountCents: 320000, rating: 3, venue: '' });
		const same = ticket({ id: 'c', kind: 'movie', amountCents: 8800, rating: 5, venue: '万达影城' });

		const s = superlatives([cheap, pricey, same], [
			{ categoryId: 7, cents: 5000, count: 2 },
			{ categoryId: 3, cents: 100, count: 1 }
		]);

		expect(s.priciest?.id).toBe('b');
		// 同为 5 星取更贵的那张
		expect(s.topRated?.id).toBe('c');
		expect(s.topVenue).toEqual({ name: '万达影城', count: 2 });
		expect(s.topCategory).toEqual({ categoryId: 7, cents: 5000, count: 2 });
	});

	it('全部未评分 → topRated 为空；场馆只去过 1 次 → topVenue 为空', () => {
		const s = superlatives([ticket({ kind: 'movie', venue: '百丽宫' })], []);
		expect(s.topRated).toBeNull();
		expect(s.topVenue).toBeNull();
		expect(s.topCategory).toBeNull();
		expect(s.priciest).not.toBeNull();
	});

	it('空票根 → 全空', () => {
		const s = superlatives([], []);
		expect(s).toEqual({ priciest: null, topRated: null, topVenue: null, topCategory: null });
	});
});

describe('ticketVenue / categoryLabel', () => {
	it('电影票 venue 为空时退回影院名', () => {
		expect(ticketVenue(ticket({ kind: 'movie', extra: { cinema: '百丽宫影城' } }))).toBe('百丽宫影城');
		expect(ticketVenue(ticket({ kind: 'movie', venue: '大光明', extra: { cinema: '百丽宫' } }))).toBe(
			'大光明'
		);
		expect(ticketVenue(ticket({ kind: 'other' }))).toBe('');
	});

	it('分类缺失时退回 分类 #id', () => {
		const cats = [{ id: 1, name: '娱乐', icon: '🎬', kind: 'expense' as const, isSystem: true, sort: 1 }];
		expect(categoryLabel(cats, 1)).toBe('🎬 娱乐');
		expect(categoryLabel(cats, 9)).toBe('分类 #9');
	});
});
