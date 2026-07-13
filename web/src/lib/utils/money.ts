import type { Direction } from '$lib/api/types';

/**
 * 分 → 元字符串：两位小数 + 千分位，不含货币符号与方向符号。
 * 纯整数运算（避免浮点误差），负数保留前导 "-"。
 *
 *   formatCents(1990)      → "19.90"
 *   formatCents(123456789) → "1,234,567.89"
 */
export function formatCents(cents: number): string {
	if (!Number.isFinite(cents)) return '0.00';
	const rounded = Math.round(cents);
	const abs = Math.abs(rounded);
	const yuan = Math.floor(abs / 100);
	const fen = String(abs % 100).padStart(2, '0');
	const grouped = String(yuan).replace(/\B(?=(\d{3})+(?!\d))/g, ',');
	return `${rounded < 0 ? '-' : ''}${grouped}.${fen}`;
}

/**
 * 带方向的完整金额串（<Amount> 的文案逻辑，design skill §1）：
 * 支出 "-¥1,234.50"，收入 "+¥88.00"。金额取绝对值，方向只由 direction 决定。
 */
export function signedAmount(cents: number, direction: Direction): string {
	return `${direction === 'expense' ? '-' : '+'}¥${formatCents(Math.abs(cents))}`;
}
