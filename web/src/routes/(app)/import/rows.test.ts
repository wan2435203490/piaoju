import { describe, expect, it } from 'vitest';
import { ApiError, ERR, type Category, type ImportRow } from '$lib/api/types';
import { mockApi } from '$lib/api/mock';
import {
	IMPORT_BATCH_SIZE,
	categoriesFor,
	categoryLabel,
	chunk,
	defaultSelection,
	previewErrorMessage,
	summarize,
	toTransactionInput,
	toggleRow
} from './rows';

const row = (over: Partial<ImportRow> & Pick<ImportRow, 'rowIndex'>): ImportRow => ({
	amountCents: 1000,
	direction: 'expense',
	occurredAt: '2026-07-13T04:12:00Z',
	note: '测试',
	paymentMethod: 'wechat',
	categoryId: 1,
	duplicate: false,
	...over
});

describe('defaultSelection（契约 §6.2：duplicate 行默认不勾选）', () => {
	it('只勾非重复行', () => {
		const rows = [row({ rowIndex: 1 }), row({ rowIndex: 2, duplicate: true }), row({ rowIndex: 3 })];
		expect(defaultSelection(rows)).toEqual([1, 3]);
	});

	it('全部重复 → 一条不勾', () => {
		expect(defaultSelection([row({ rowIndex: 1, duplicate: true })])).toEqual([]);
	});
});

describe('toggleRow', () => {
	it('未选 → 选中；已选 → 取消', () => {
		expect(toggleRow([1, 2], 3)).toEqual([1, 2, 3]);
		expect(toggleRow([1, 2, 3], 2)).toEqual([1, 3]);
	});
});

describe('summarize（支出/收入分开合计，整数分）', () => {
	const rows = [
		row({ rowIndex: 1, amountCents: 2800 }),
		row({ rowIndex: 2, amountCents: 1600 }),
		row({ rowIndex: 3, amountCents: 400, duplicate: true }),
		row({ rowIndex: 4, amountCents: 5000, direction: 'income', categoryId: 10 })
	];

	it('只统计已勾选的行', () => {
		const s = summarize(rows, defaultSelection(rows)); // 3 号重复未勾
		expect(s).toEqual({
			total: 4,
			selected: 3,
			skipped: 1,
			duplicates: 1,
			expenseCents: 4400,
			incomeCents: 5000
		});
	});

	it('一条不勾 → 合计为 0，跳过数 = 总数', () => {
		const s = summarize(rows, []);
		expect(s.selected).toBe(0);
		expect(s.skipped).toBe(4);
		expect(s.expenseCents).toBe(0);
		expect(s.incomeCents).toBe(0);
	});
});

describe('chunk（大批量分批提交）', () => {
	it('按批次切分，最后一批可不满', () => {
		expect(chunk([1, 2, 3, 4, 5], 2)).toEqual([[1, 2], [3, 4], [5]]);
	});

	it('空数组 → 无批次；默认批大小生效', () => {
		expect(chunk([])).toEqual([]);
		const rows = Array.from({ length: IMPORT_BATCH_SIZE + 1 }, (_, i) => i);
		expect(chunk(rows).length).toBe(2);
	});
});

describe('toTransactionInput（契约 §4 TransactionInput 逐字段）', () => {
	it('id 由调用方传入；分类默认取服务端建议值', () => {
		const input = toTransactionInput(row({ rowIndex: 1 }), 'fixed-uuid');
		expect(input).toEqual({
			id: 'fixed-uuid',
			amountCents: 1000,
			direction: 'expense',
			categoryId: 1,
			note: '测试',
			occurredAt: '2026-07-13T04:12:00Z',
			paymentMethod: 'wechat'
		});
	});

	it('用户改过分类 → override 覆盖建议值', () => {
		expect(toTransactionInput(row({ rowIndex: 1 }), 'x', 5).categoryId).toBe(5);
	});
});

describe('分类展示', () => {
	const categories: Category[] = [
		{ id: 1, name: '餐饮', icon: '🍜', kind: 'expense', isSystem: true, sort: 1 },
		{ id: 10, name: '红包', icon: '🧧', kind: 'income', isSystem: true, sort: 2 }
	];

	it('按方向过滤（支出行只给支出分类）', () => {
		expect(categoriesFor(categories, 'expense').map((c) => c.id)).toEqual([1]);
		expect(categoriesFor(categories, 'income').map((c) => c.id)).toEqual([10]);
	});

	it('未知分类 → 未分类兜底', () => {
		expect(categoryLabel(categories, 1)).toBe('🍜 餐饮');
		expect(categoryLabel(categories, 999)).toBe('未分类');
	});
});

describe('previewErrorMessage（契约 §6.2 错误码）', () => {
	it('40001 → 指出来源选错', () => {
		const msg = previewErrorMessage(new ApiError(ERR.VALIDATION, 'bad csv'), 'alipay');
		expect(msg).toContain('支付宝');
		expect(msg).toContain('账单格式');
	});

	it('41301 → 文件超限', () => {
		expect(previewErrorMessage(new ApiError(ERR.UPLOAD_REJECTED, 'too big'), 'wechat')).toContain(
			'5MB'
		);
	});

	it('非 ApiError → 通用兜底', () => {
		expect(previewErrorMessage(new Error('boom'), 'wechat')).toBe('解析失败，请稍后重试');
	});
});

describe('mock 契约行为（VITE_MOCK=1 下两个功能都要能走通）', () => {
	it('识票：未上传的附件 → 40401', async () => {
		await expect(mockApi.recognizeTicket(999)).rejects.toBeInstanceOf(ApiError);
	});

	it('识票：上传后可识别，返回一张电影票草稿（confidence ≥ 0.6）', async () => {
		const attachment = await mockApi.upload(new Blob(['x']));
		const draft = await mockApi.recognizeTicket(attachment.id);
		expect(draft.kind).toBe('movie');
		expect(draft.amountCents).toBeGreaterThan(0);
		expect(draft.confidence).toBeGreaterThanOrEqual(0.6);
		expect(draft.extra).toHaveProperty('cinema');
	});

	it('导入预览：10 行含 2 行疑似重复，paymentMethod 跟随来源', async () => {
		const csv = new File(['时间,金额\n'], 'bill.csv', { type: 'text/csv' });
		const preview = await mockApi.previewImport(csv, 'alipay');
		expect(preview.total).toBe(10);
		expect(preview.items.length).toBe(10);
		expect(preview.duplicates).toBe(2);
		expect(preview.items.filter((r) => r.duplicate).length).toBe(2);
		expect(preview.items.every((r) => r.paymentMethod === 'alipay')).toBe(true);
		// 疑似重复默认不导入 → 默认勾选 8 条
		expect(defaultSelection(preview.items).length).toBe(8);
	});

	it('导入预览：非 CSV → 40001；超 5MB → 41301', async () => {
		const png = new File(['x'], 'bill.png', { type: 'image/png' });
		await expect(mockApi.previewImport(png, 'wechat')).rejects.toMatchObject({
			code: ERR.VALIDATION
		});
		const big = new File([new Uint8Array(5 * 1024 * 1024 + 1)], 'big.csv', { type: 'text/csv' });
		await expect(mockApi.previewImport(big, 'wechat')).rejects.toMatchObject({
			code: ERR.UPLOAD_REJECTED
		});
	});
});
