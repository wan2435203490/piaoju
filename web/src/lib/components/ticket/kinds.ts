/**
 * TicketCard 家族共享的票型元数据 + 展示/表单工具函数（W3 所有权目录）。
 * - 色标只引用 app.css 的 --kind-* token（design skill §1，禁止裸色值）
 * - 时间：API 一律 RFC3339 UTC，这里负责「展示转本地」与
 *   「datetime-local 输入值 ↔ RFC3339」双向转换（conventions §1）
 * - 金额：整数分 ↔ 表单元字符串，纯整数运算避免浮点误差
 */
import type { PaymentMethod, Ticket, TicketKind } from '$lib/api/types';
import { isKind } from '$lib/api/types';

/* ============ 票型元数据 ============ */

export interface KindMeta {
	label: string;
	emoji: string;
	/** app.css 票型色标 token 引用（直接可用于 style 值） */
	color: string;
}

export const KIND_META: Record<TicketKind, KindMeta> = {
	movie: { label: '电影', emoji: '🎬', color: 'var(--kind-movie)' },
	show: { label: '演出', emoji: '🎭', color: 'var(--kind-show)' },
	attraction: { label: '门票', emoji: '🏞️', color: 'var(--kind-attraction)' },
	train: { label: '车票', emoji: '🚄', color: 'var(--kind-train)' },
	flight: { label: '机票', emoji: '✈️', color: 'var(--kind-flight)' },
	other: { label: '其他', emoji: '🎫', color: 'var(--kind-other)' }
};

export const PAYMENT_LABEL: Record<PaymentMethod, string> = {
	wechat: '微信支付',
	alipay: '支付宝',
	cash: '现金',
	card: '银行卡',
	other: '其他'
};

/* ============ extra 字段表（PROTOCOL §5 逐字段对齐，表单与详情共用） ============ */

export interface ExtraFieldDef {
	key: string;
	label: string;
	placeholder?: string;
	/** datetime → datetime-local 输入 + RFC3339 存储；缺省为文本 */
	type?: 'text' | 'datetime';
}

export const EXTRA_FIELDS: Record<TicketKind, ExtraFieldDef[]> = {
	movie: [
		{ key: 'cinema', label: '影院' },
		{ key: 'hall', label: '影厅' },
		{ key: 'filmFormat', label: '制式', placeholder: 'IMAX / 杜比…' }
	],
	show: [
		{ key: 'tour', label: '巡演', placeholder: '巡演/剧目名' },
		{ key: 'session', label: '场次' },
		{ key: 'zone', label: '区域' }
	],
	attraction: [
		{ key: 'city', label: '城市' },
		{ key: 'ticketType', label: '票种', placeholder: '成人 / 学生…' }
	],
	train: [
		{ key: 'trainNo', label: '车次', placeholder: 'G102' },
		{ key: 'fromStation', label: '出发站' },
		{ key: 'toStation', label: '到达站' },
		{ key: 'departTime', label: '出发时间', type: 'datetime' },
		{ key: 'arriveTime', label: '到达时间', type: 'datetime' },
		{ key: 'seatClass', label: '坐席', placeholder: '二等座' }
	],
	flight: [
		{ key: 'flightNo', label: '航班号', placeholder: 'MU5137' },
		{ key: 'airline', label: '航空公司' },
		{ key: 'fromAirport', label: '出发机场' },
		{ key: 'toAirport', label: '到达机场' },
		{ key: 'departTime', label: '起飞时间', type: 'datetime' },
		{ key: 'arriveTime', label: '到达时间', type: 'datetime' },
		{ key: 'cabin', label: '舱位', placeholder: '经济舱' }
	],
	other: []
};

/* ============ 时间：RFC3339 UTC ↔ 本地展示 / datetime-local ============ */

const pad = (n: number) => String(n).padStart(2, '0');

function parseIso(iso: string): Date | null {
	if (!iso) return null;
	const d = new Date(iso);
	return Number.isNaN(d.getTime()) ? null : d;
}

/** → "2026-07-12 19:30"（本地时区） */
export function fmtDateTime(iso: string): string {
	const d = parseIso(iso);
	if (!d) return '';
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** → "2026-07-12"（本地时区） */
export function fmtDate(iso: string): string {
	const d = parseIso(iso);
	if (!d) return '';
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** → "19:30"（本地时区） */
export function fmtTime(iso: string): string {
	const d = parseIso(iso);
	if (!d) return '';
	return `${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** → "2026年7月"（时间线分组标题，本地时区） */
export function fmtMonth(iso: string): string {
	const d = parseIso(iso);
	if (!d) return '';
	return `${d.getFullYear()}年${d.getMonth() + 1}月`;
}

/** RFC3339 → <input type="datetime-local"> 值（本地时区）；空/非法 → '' */
export function isoToLocalInput(iso: string): string {
	const d = parseIso(iso);
	if (!d) return '';
	return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

/** datetime-local 值 → RFC3339 UTC；空/非法 → '' */
export function localInputToIso(value: string): string {
	const d = parseIso(value);
	return d ? d.toISOString() : '';
}

/** 当前时刻的 datetime-local 值（表单默认「时间默认现在」，design §4） */
export function nowLocalInput(): string {
	return isoToLocalInput(new Date().toISOString());
}

/* ============ 金额：分 ↔ 表单「元」字符串 ============ */

/** "45" / "45.9" / "45.90" → 4590；非法/负数 → null（纯整数运算） */
export function parseYuanToCents(input: string): number | null {
	const t = input.trim();
	if (!/^\d+(\.\d{1,2})?$/.test(t)) return null;
	const [yuan = '0', fen = ''] = t.split('.');
	return Number(yuan) * 100 + Number((fen + '00').slice(0, 2));
}

/** 4590 → "45.90"（表单回填用，不带千分位） */
export function centsToYuanInput(cents: number): string {
	const abs = Math.abs(Math.round(cents));
	return `${Math.floor(abs / 100)}.${pad(abs % 100)}`;
}

/* ============ 卡片/详情的派生文案 ============ */

const joinParts = (parts: (string | undefined)[]): string =>
	parts.filter((p): p is string => !!p && p.trim() !== '').join(' · ');

/** 票根卡第二行（场馆 + 票型专属信息），按 kind 组合 */
export function stubMeta(ticket: Ticket): string {
	if (isKind(ticket, 'movie')) {
		return joinParts([ticket.extra.cinema || ticket.venue, ticket.extra.hall, ticket.extra.filmFormat]);
	}
	if (isKind(ticket, 'show')) {
		return joinParts([ticket.venue, ticket.extra.tour, ticket.extra.zone]);
	}
	if (isKind(ticket, 'attraction')) {
		return joinParts([ticket.venue, ticket.extra.ticketType]);
	}
	return ticket.venue;
}

export interface DetailRow {
	label: string;
	value: string;
}

/** 详情页「票面信息」的 extra 行（空字段自动省略；时间字段转本地展示） */
export function extraRows(ticket: Ticket): DetailRow[] {
	const src = ticket.extra as Record<string, string>;
	return EXTRA_FIELDS[ticket.kind]
		.map((def) => ({
			label: def.label,
			value: def.type === 'datetime' ? fmtDateTime(src[def.key] ?? '') : (src[def.key] ?? '')
		}))
		.filter((row) => row.value !== '');
}
