import { describe, expect, it } from 'vitest';
import { formatCents, signedAmount } from './money';

describe('formatCents（分 → 元）', () => {
	it('零与不足一元', () => {
		expect(formatCents(0)).toBe('0.00');
		expect(formatCents(5)).toBe('0.05');
		expect(formatCents(50)).toBe('0.50');
	});

	it('常规金额两位小数', () => {
		expect(formatCents(1990)).toBe('19.90');
		expect(formatCents(4500)).toBe('45.00');
		expect(formatCents(9900)).toBe('99.00');
	});

	it('千分位分组', () => {
		expect(formatCents(185000)).toBe('1,850.00');
		expect(formatCents(1850000)).toBe('18,500.00');
		expect(formatCents(123456789)).toBe('1,234,567.89');
	});

	it('负数保留符号', () => {
		expect(formatCents(-2500)).toBe('-25.00');
	});

	it('非法输入兜底', () => {
		expect(formatCents(Number.NaN)).toBe('0.00');
	});
});

describe('signedAmount（方向符号 + 货币符号）', () => {
	it('支出前缀 -¥', () => {
		expect(signedAmount(4500, 'expense')).toBe('-¥45.00');
	});

	it('收入前缀 +¥', () => {
		expect(signedAmount(1850000, 'income')).toBe('+¥18,500.00');
	});

	it('方向只由 direction 决定（金额取绝对值）', () => {
		expect(signedAmount(-880, 'income')).toBe('+¥8.80');
	});
});
