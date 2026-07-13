import { describe, expect, it } from 'vitest';
import { applyKey, evaluateCents, isCompound } from './expression';

/** 依次按键，得到最终表达式 */
function type(keys: string): string {
	let expr = '';
	for (const key of keys) {
		expr = applyKey(expr, key as Parameters<typeof applyKey>[1]);
	}
	return expr;
}

describe('applyKey（按键状态机）', () => {
	it('数字追加', () => {
		expect(type('128')).toBe('128');
	});

	it('前导 0 被覆盖，"0." 合法', () => {
		expect(type('05')).toBe('5');
		expect(type('0.5')).toBe('0.5');
	});

	it('空项按 "." 自动补 "0."', () => {
		expect(type('.5')).toBe('0.5');
		expect(type('12+.5')).toBe('12+0.5');
	});

	it('同一项内第二个 "." 无效', () => {
		expect(type('1.2.3')).toBe('1.23');
	});

	it('小数最多两位', () => {
		expect(type('1.234')).toBe('1.23');
	});

	it('整数最多 7 位', () => {
		expect(type('123456789')).toBe('1234567');
	});

	it('"+" 只能接在数字后', () => {
		expect(type('+1')).toBe('1');
		expect(type('1++2')).toBe('1+2');
		expect(type('1.+2')).toBe('1.2'); // "1." 后 + 无效，2 继续拼小数
	});

	it('backspace 逐字符删除', () => {
		expect(applyKey('12+8', 'backspace')).toBe('12+');
		expect(applyKey('', 'backspace')).toBe('');
	});
});

describe('evaluateCents（算式 → 整数分）', () => {
	it('单项', () => {
		expect(evaluateCents('')).toBe(0);
		expect(evaluateCents('12')).toBe(1200);
		expect(evaluateCents('12.5')).toBe(1250);
		expect(evaluateCents('0.99')).toBe(99);
	});

	it('求和（键盘即算式）', () => {
		expect(evaluateCents('12+8')).toBe(2000);
		expect(evaluateCents('0.99+0.01')).toBe(100);
		expect(evaluateCents('1+2+3.5')).toBe(650);
	});

	it('容忍尾随运算符 / 小数点', () => {
		expect(evaluateCents('12+')).toBe(1200);
		expect(evaluateCents('12.')).toBe(1200);
	});

	it('单位小数补零（"1.5" = 1 元 5 角）', () => {
		expect(evaluateCents('1.5')).toBe(150);
		expect(evaluateCents('1.05')).toBe(105);
	});
});

describe('isCompound（是否显示合计预览）', () => {
	it('单项不算复合', () => {
		expect(isCompound('12')).toBe(false);
		expect(isCompound('12+')).toBe(false);
		expect(isCompound('12+0')).toBe(false);
	});

	it('两个以上非零项才算', () => {
		expect(isCompound('12+8')).toBe(true);
		expect(isCompound('1+2+3')).toBe(true);
	});
});
