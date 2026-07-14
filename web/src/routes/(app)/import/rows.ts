/**
 * 账单导入的纯逻辑（PROTOCOL §6.2）——页面只负责渲染，规则全在这里，单测覆盖。
 *
 * 关键约定：
 * - duplicate=true 的行默认不勾选（契约 §6.2「建议跳过」）
 * - 每行一条 Transaction，写入一律走 outbox.createTransaction（离线安全 + 服务端幂等 upsert）
 * - 行 id 在预览阶段就生成并固定：导入失败重试时复用同一 id → 幂等，不会写重
 * - 大批量分批提交（IMPORT_BATCH_SIZE），每批之间让出主线程刷新进度条，不卡死 UI
 */
import { ApiError, ERR, type Category, type Direction, type ImportRow, type ImportSource, type TransactionInput } from '$lib/api/types';

/** 每批提交条数（几百行时分批 + 进度条，避免长任务卡 UI） */
export const IMPORT_BATCH_SIZE = 20;

/**
 * 预览列表最多渲染的行数（契约 §6.2 只限文件 5MB，一份账单可达数万行）。
 * 超出只展示前 N 条供核对，未展示的行按建议默认全量导入 —— 避免几万个 <li>
 * 一次性铺满 DOM 把主线程冻住（ANR/白屏）。核对/改分类需要时请按月份分开导出。
 */
export const IMPORT_PREVIEW_LIMIT = 500;

export const SOURCE_LABEL: Record<ImportSource, string> = {
	wechat: '微信支付',
	alipay: '支付宝'
};

export const SOURCE_EMOJI: Record<ImportSource, string> = {
	wechat: '💬',
	alipay: '🅰️'
};

/** 默认勾选集合：疑似重复的行默认不勾（契约 §6.2） */
export function defaultSelection(rows: ImportRow[]): number[] {
	return rows.filter((row) => !row.duplicate).map((row) => row.rowIndex);
}

/** 勾选/取消单行 */
export function toggleRow(selected: readonly number[], rowIndex: number): number[] {
	return selected.includes(rowIndex)
		? selected.filter((i) => i !== rowIndex)
		: [...selected, rowIndex];
}

export interface ImportSummary {
	total: number;
	/** 已勾选（将写入）的行数 */
	selected: number;
	/** 未勾选（跳过）的行数 */
	skipped: number;
	/** 疑似重复的行数 */
	duplicates: number;
	/** 已勾选行的支出/收入合计（整数分） */
	expenseCents: number;
	incomeCents: number;
}

export function summarize(rows: readonly ImportRow[], selected: readonly number[]): ImportSummary {
	const picked = new Set(selected);
	let expenseCents = 0;
	let incomeCents = 0;
	let selectedCount = 0;
	for (const row of rows) {
		if (!picked.has(row.rowIndex)) continue;
		selectedCount++;
		if (row.direction === 'expense') expenseCents += row.amountCents;
		else incomeCents += row.amountCents;
	}
	return {
		total: rows.length,
		selected: selectedCount,
		skipped: rows.length - selectedCount,
		duplicates: rows.filter((row) => row.duplicate).length,
		expenseCents,
		incomeCents
	};
}

/** 分批（最后一批可能不满） */
export function chunk<T>(items: readonly T[], size: number = IMPORT_BATCH_SIZE): T[][] {
	const step = Math.max(1, Math.floor(size));
	const out: T[][] = [];
	for (let i = 0; i < items.length; i += step) out.push(items.slice(i, i + step));
	return out;
}

/**
 * 单行 → TransactionInput（契约 §4）。
 * id 由调用方传入（$lib/utils/uuid 客户端生成，行内固定不变 → 重试幂等）；
 * categoryId 优先取用户改过的值，否则用服务端规则建议值。
 */
export function toTransactionInput(
	row: ImportRow,
	id: string,
	categoryOverride?: number
): TransactionInput {
	return {
		id,
		amountCents: row.amountCents,
		direction: row.direction,
		categoryId: categoryOverride ?? row.categoryId,
		note: row.note,
		occurredAt: row.occurredAt,
		paymentMethod: row.paymentMethod
	};
}

/** 按方向过滤可选分类（支出行只给支出分类，反之亦然） */
export function categoriesFor(categories: readonly Category[], direction: Direction): Category[] {
	return categories.filter((category) => category.kind === direction);
}

/** 分类名兜底：找不到（分类被删/未加载）时显示「未分类」 */
export function categoryLabel(categories: readonly Category[], categoryId: number): string {
	const hit = categories.find((category) => category.id === categoryId);
	return hit ? `${hit.icon} ${hit.name}` : '未分类';
}

/** 预览接口的错误 → 用户能懂的话（契约 §6.2：40001 / 41301） */
export function previewErrorMessage(error: unknown, source: ImportSource): string {
	if (error instanceof ApiError) {
		if (error.code === ERR.VALIDATION) {
			return `不是${SOURCE_LABEL[source]}的账单格式，请确认导出来源后重试`;
		}
		if (error.code === ERR.UPLOAD_REJECTED) {
			return '账单文件超过 5MB 限制，请按月份分开导出后再试';
		}
		return error.message || '解析失败，请稍后重试';
	}
	return '解析失败，请稍后重试';
}
