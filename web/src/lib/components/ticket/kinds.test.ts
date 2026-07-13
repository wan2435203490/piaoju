import { describe, expect, it } from 'vitest';
import type { Ticket } from '$lib/api/types';
import {
	EXTRA_FIELDS,
	centsToYuanInput,
	extraRows,
	fmtDateTime,
	isoToLocalInput,
	localInputToIso,
	parseYuanToCents,
	stubMeta
} from './kinds';

describe('parseYuanToCents（元字符串 → 整数分，纯整数运算）', () => {
	it('整数 / 一位小数 / 两位小数', () => {
		expect(parseYuanToCents('45')).toBe(4500);
		expect(parseYuanToCents('45.9')).toBe(4590);
		expect(parseYuanToCents('45.90')).toBe(4590);
		expect(parseYuanToCents('0.01')).toBe(1);
	});

	it('浮点陷阱值不失真（19.90 → 1990）', () => {
		expect(parseYuanToCents('19.90')).toBe(1990);
		expect(parseYuanToCents('0.29')).toBe(29);
	});

	it('非法输入 → null', () => {
		expect(parseYuanToCents('')).toBeNull();
		expect(parseYuanToCents('abc')).toBeNull();
		expect(parseYuanToCents('1.234')).toBeNull();
		expect(parseYuanToCents('-5')).toBeNull();
		expect(parseYuanToCents('1+2')).toBeNull();
	});
});

describe('centsToYuanInput（分 → 表单回填元串）', () => {
	it('两位小数、无千分位', () => {
		expect(centsToYuanInput(4590)).toBe('45.90');
		expect(centsToYuanInput(100)).toBe('1.00');
		expect(centsToYuanInput(9)).toBe('0.09');
		expect(centsToYuanInput(128000)).toBe('1280.00');
	});

	it('与 parseYuanToCents 互为往返', () => {
		for (const cents of [1, 99, 100, 4590, 62300, 128000]) {
			expect(parseYuanToCents(centsToYuanInput(cents))).toBe(cents);
		}
	});
});

describe('时间转换（RFC3339 UTC ↔ datetime-local，本地时区无关的往返）', () => {
	it('iso → local input → iso 保持同一时刻', () => {
		const iso = '2026-07-11T11:30:00.000Z';
		const local = isoToLocalInput(iso);
		expect(local).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/);
		expect(localInputToIso(local)).toBe(iso);
	});

	it('空值与非法值 → 空串', () => {
		expect(isoToLocalInput('')).toBe('');
		expect(localInputToIso('')).toBe('');
		expect(fmtDateTime('not-a-date')).toBe('');
	});
});

const baseTicket = {
	id: '5cae4524-f800-4ac5-b481-09410cab1a59',
	title: '沙丘 3',
	venue: '万达影城',
	eventTime: '2026-07-11T11:30:00Z',
	seat: '9排12座',
	rating: 5,
	memo: '',
	transaction: {
		id: 't1',
		amountCents: 9900,
		categoryId: 5,
		paymentMethod: 'wechat' as const
	},
	attachments: [],
	createdAt: '2026-07-11T11:35:00Z',
	updatedAt: '2026-07-11T15:02:00Z'
};

describe('stubMeta（票根卡第二行文案按 kind 组合）', () => {
	it('movie：影院 · 影厅 · 制式', () => {
		const ticket: Ticket = {
			...baseTicket,
			kind: 'movie',
			extra: { cinema: '万达影城', hall: 'IMAX 激光厅', filmFormat: 'IMAX 2D' }
		};
		expect(stubMeta(ticket)).toBe('万达影城 · IMAX 激光厅 · IMAX 2D');
	});

	it('空字段自动省略', () => {
		const ticket: Ticket = {
			...baseTicket,
			kind: 'attraction',
			extra: { city: '', ticketType: '' }
		};
		expect(stubMeta(ticket)).toBe('万达影城'); // 仅剩 venue
	});
});

describe('extraRows（详情页票面信息行 = PROTOCOL §5 extra 表）', () => {
	it('train：六字段齐全时全部输出，datetime 字段转本地展示', () => {
		const ticket: Ticket = {
			...baseTicket,
			kind: 'train',
			extra: {
				trainNo: 'G102',
				fromStation: '杭州东',
				toStation: '北京南',
				departTime: '2026-07-08T01:03:00Z',
				arriveTime: '2026-07-08T05:27:00Z',
				seatClass: '二等座'
			}
		};
		const rows = extraRows(ticket);
		expect(rows.map((r) => r.label)).toEqual([
			'车次',
			'出发站',
			'到达站',
			'出发时间',
			'到达时间',
			'坐席'
		]);
		// datetime 字段被格式化为 "YYYY-MM-DD HH:mm"（本地时区）
		const depart = rows.find((r) => r.label === '出发时间');
		expect(depart?.value).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
	});

	it('other：无 extra 行', () => {
		const ticket: Ticket = { ...baseTicket, kind: 'other', extra: {} };
		expect(extraRows(ticket)).toEqual([]);
	});
});

describe('EXTRA_FIELDS 与 PROTOCOL §5 extra 表逐字段对齐', () => {
	it('五种 kind 的 key 集合完全一致', () => {
		expect(EXTRA_FIELDS.movie.map((d) => d.key)).toEqual(['cinema', 'hall', 'filmFormat']);
		expect(EXTRA_FIELDS.show.map((d) => d.key)).toEqual(['tour', 'session', 'zone']);
		expect(EXTRA_FIELDS.attraction.map((d) => d.key)).toEqual(['city', 'ticketType']);
		expect(EXTRA_FIELDS.train.map((d) => d.key)).toEqual([
			'trainNo',
			'fromStation',
			'toStation',
			'departTime',
			'arriveTime',
			'seatClass'
		]);
		expect(EXTRA_FIELDS.flight.map((d) => d.key)).toEqual([
			'flightNo',
			'airline',
			'fromAirport',
			'toAirport',
			'departTime',
			'arriveTime',
			'cabin'
		]);
		expect(EXTRA_FIELDS.other).toEqual([]);
	});
});
