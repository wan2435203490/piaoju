/**
 * 快记键盘算式（piaoju-design §4「键盘即算式」：支持 `12+8` 直接求和）。
 *
 * 表达式 = 若干「金额项」用 + 连接（如 "12.5+8+0.5"），每项最多
 * 7 位整数 + 2 位小数。求值全程整数「分」运算，避免浮点误差
 * （conventions §1：金额一律整数分）。
 */
import type { NumPadKey } from '$lib/components/NumPad.svelte';

const MAX_INT_DIGITS = 7; // 单项上限 9,999,999 元
const MAX_DECIMALS = 2;

/** 取最后一个「+」之后的当前输入项 */
function currentTerm(expr: string): string {
	const i = expr.lastIndexOf('+');
	return i === -1 ? expr : expr.slice(i + 1);
}

/** 按键 → 新表达式（非法按键一律原样返回，不抛错不弹提示） */
export function applyKey(expr: string, key: NumPadKey): string {
	if (key === 'backspace') return expr.slice(0, -1);

	const term = currentTerm(expr);

	if (key === '+') {
		// 只允许接在数字之后（"" / "12+" / "12." 后按 + 无效）
		return /\d$/.test(expr) ? `${expr}+` : expr;
	}

	if (key === '.') {
		if (term.includes('.')) return expr;
		return term === '' ? `${expr}0.` : `${expr}.`;
	}

	// 数字键
	const dot = term.indexOf('.');
	if (dot === -1) {
		if (term === '0') return expr.slice(0, -1) + key; // "0" → 直接覆盖，避免 "05"
		if (term.length >= MAX_INT_DIGITS) return expr;
	} else if (term.length - dot - 1 >= MAX_DECIMALS) {
		return expr;
	}
	return expr + key;
}

/** 单项 → 分："12.5" → 1250；空项 / "." → 0 */
function termCents(term: string): number {
	if (term === '' || term === '.') return 0;
	const [int = '', dec = ''] = term.split('.');
	const yuan = int === '' ? 0 : Number.parseInt(int, 10);
	const fen = dec === '' ? 0 : Number.parseInt(dec.padEnd(MAX_DECIMALS, '0').slice(0, MAX_DECIMALS), 10);
	return yuan * 100 + fen;
}

/** 整条算式求值 → 整数分（容忍尾随 "+" / "."） */
export function evaluateCents(expr: string): number {
	return expr.split('+').reduce((sum, term) => sum + termCents(term), 0);
}

/** 是否为复合算式（≥2 个非零项，需要显示「= 合计」预览） */
export function isCompound(expr: string): boolean {
	return expr.split('+').filter((term) => termCents(term) > 0).length >= 2;
}
