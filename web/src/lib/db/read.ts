/**
 * 本地读取层（W5）—— 离线时页面的数据来源。
 *
 * 语义与服务端对齐（契约 §3/§4/§5/§7），两点刻意的差异：
 * - **不做游标分页**：本地库是已同步数据的完整镜像，一个月的流水量级在几十~几百条，
 *   直接按条件全量返回、nextCursor 恒为 null。页面的「加载更多」自然不触发。
 * - **统计本地聚合**：口径同契约 §7（byCategory/byDay 仅 expense），离线也能看统计。
 *
 * 所有查询都过滤墓碑（deletedAt 非 null 的行本地保留，见 schema.ts）。
 */
import type {
	ListPage,
	MonthlyStats,
	TicketKind,
	TicketQuery,
	TicketStats,
	TransactionQuery
} from '$lib/api/types';
import type { LocalCategory, LocalTicket, LocalTransaction, PiaojuDB } from './schema';

/** 该行是否可见（未被软删） */
function alive<T extends { deletedAt?: string | null }>(row: T): boolean {
	return !row.deletedAt;
}

/** month="2026-07" → [start, end) 的 ISO 边界（UTC，与服务端口径一致） */
function monthRange(month: string): [string, string] {
	const [y, m] = month.split('-').map(Number);
	const start = new Date(Date.UTC(y!, m! - 1, 1));
	const end = new Date(Date.UTC(y!, m!, 1));
	return [start.toISOString(), end.toISOString()];
}

export async function listTransactions(
	db: PiaojuDB,
	q: TransactionQuery = {}
): Promise<ListPage<LocalTransaction>> {
	let rows = await db.transactions.toArray();
	rows = rows.filter(alive);

	if (q.month) {
		const [start, end] = monthRange(q.month);
		rows = rows.filter((r) => r.occurredAt >= start && r.occurredAt < end);
	}
	if (q.categoryId !== undefined) rows = rows.filter((r) => r.categoryId === q.categoryId);
	if (q.direction) rows = rows.filter((r) => r.direction === q.direction);

	rows.sort((a, b) => (a.occurredAt < b.occurredAt ? 1 : a.occurredAt > b.occurredAt ? -1 : 0));
	return { items: rows, nextCursor: null };
}

export async function listTickets(db: PiaojuDB, q: TicketQuery = {}): Promise<ListPage<LocalTicket>> {
	let rows = await db.tickets.toArray();
	rows = rows.filter(alive);

	if (q.kind) rows = rows.filter((r) => r.kind === q.kind);
	if (q.year !== undefined) rows = rows.filter((r) => new Date(r.eventTime).getUTCFullYear() === q.year);

	rows.sort((a, b) => (a.eventTime < b.eventTime ? 1 : a.eventTime > b.eventTime ? -1 : 0));
	return { items: rows, nextCursor: null };
}

export async function getTicket(db: PiaojuDB, id: string): Promise<LocalTicket | null> {
	const row = await db.tickets.get(id);
	return row && alive(row) ? row : null;
}

export async function listCategories(db: PiaojuDB): Promise<LocalCategory[]> {
	const rows = (await db.categories.toArray()).filter(alive);
	rows.sort((a, b) => (a.kind === b.kind ? a.sort - b.sort : a.kind < b.kind ? -1 : 1));
	return rows;
}

/** 月度统计：口径同契约 §7 —— byCategory/byDay 只统计 expense，两向总额分开给 */
export async function statsMonthly(db: PiaojuDB, month: string): Promise<MonthlyStats> {
	const { items } = await listTransactions(db, { month });

	let expenseCents = 0;
	let incomeCents = 0;
	const byCategoryMap = new Map<number, { cents: number; count: number }>();
	const byDayMap = new Map<string, number>();

	for (const tx of items) {
		if (tx.direction === 'income') {
			incomeCents += tx.amountCents;
			continue;
		}
		expenseCents += tx.amountCents;

		const cat = byCategoryMap.get(tx.categoryId) ?? { cents: 0, count: 0 };
		cat.cents += tx.amountCents;
		cat.count += 1;
		byCategoryMap.set(tx.categoryId, cat);

		const day = tx.occurredAt.slice(0, 10); // YYYY-MM-DD（UTC，同服务端）
		byDayMap.set(day, (byDayMap.get(day) ?? 0) + tx.amountCents);
	}

	return {
		expenseCents,
		incomeCents,
		byCategory: [...byCategoryMap].map(([categoryId, v]) => ({ categoryId, ...v })),
		byDay: [...byDayMap]
			.map(([date, expenseCents]) => ({ date, expenseCents }))
			.sort((a, b) => (a.date < b.date ? -1 : 1))
	};
}

/** 票据年度统计（契约 §7） */
export async function statsTickets(db: PiaojuDB, year: number): Promise<TicketStats> {
	const { items } = await listTickets(db, { year });
	const byKindMap = new Map<TicketKind, { count: number; cents: number }>();

	for (const t of items) {
		const k = byKindMap.get(t.kind) ?? { count: 0, cents: 0 };
		k.count += 1;
		k.cents += t.transaction.amountCents;
		byKindMap.set(t.kind, k);
	}

	return {
		total: items.length,
		byKind: [...byKindMap].map(([kind, v]) => ({ kind, ...v }))
	};
}

/** 待同步条数（UI 顶栏标记用） */
export async function pendingCount(db: PiaojuDB): Promise<number> {
	return db.outbox.count();
}
