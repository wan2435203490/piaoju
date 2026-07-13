/**
 * 记账模块展示层格式化（月份切换、日分组、时刻、支付方式、图表刻度）。
 * API 时间一律 RFC3339 UTC，这里全部转「本地时区」展示（conventions §1）。
 */
import type { PaymentMethod } from '$lib/api/types';

export const PAYMENT_LABELS: Record<PaymentMethod, string> = {
	wechat: '微信',
	alipay: '支付宝',
	cash: '现金',
	card: '银行卡',
	other: '其他'
};

const WEEKDAYS = ['日', '一', '二', '三', '四', '五', '六'] as const;

const pad2 = (n: number): string => String(n).padStart(2, '0');

const dateKey = (d: Date): string => `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;

/** 本地「今天」所在月份 "YYYY-MM" */
export function currentMonth(): string {
	const now = new Date();
	return `${now.getFullYear()}-${pad2(now.getMonth() + 1)}`;
}

/** "2026-07" ± n 月 */
export function addMonths(month: string, delta: number): string {
	const [y = 0, m = 1] = month.split('-').map(Number);
	const d = new Date(y, m - 1 + delta, 1);
	return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}`;
}

/** "2026-07" → "2026年7月" */
export function monthTitle(month: string): string {
	const [y = 0, m = 1] = month.split('-').map(Number);
	return `${y}年${m}月`;
}

/** 当月全部日期 key（"YYYY-MM-DD"） */
export function monthDays(month: string): string[] {
	const [y = 0, m = 1] = month.split('-').map(Number);
	const count = new Date(y, m, 0).getDate();
	return Array.from({ length: count }, (_, i) => `${month}-${pad2(i + 1)}`);
}

/** RFC3339 UTC → 本地日期 key "YYYY-MM-DD"（日分组用） */
export function localDayKey(iso: string): string {
	return dateKey(new Date(iso));
}

/** 日分组标题：今天 / 昨天 / M月D日 周X */
export function dayHeading(key: string): string {
	const now = new Date();
	if (key === dateKey(now)) return '今天';
	const yesterday = new Date(now.getFullYear(), now.getMonth(), now.getDate() - 1);
	if (key === dateKey(yesterday)) return '昨天';
	const [y = 0, m = 1, d = 1] = key.split('-').map(Number);
	const weekday = WEEKDAYS[new Date(y, m - 1, d).getDay()] ?? '';
	return `${m}月${d}日 周${weekday}`;
}

/** "2026-07-08" → "7月8日"（图表读数行用） */
export function dayShort(key: string): string {
	const [, m = 1, d = 1] = key.split('-').map(Number);
	return `${m}月${d}日`;
}

/** RFC3339 UTC → 本地 "HH:mm" */
export function timeHM(iso: string): string {
	const d = new Date(iso);
	return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`;
}

/** 图表刻度/直标：分 → 整数元（四舍五入 + 千分位，无小数） */
export function yuanCompact(cents: number): string {
	return String(Math.round(cents / 100)).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
}

/** Svelte JS transition 时长（尊重 prefers-reduced-motion，同 Sheet 的做法） */
export function motionDur(ms: number): number {
	return typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
		? 0
		: ms;
}
