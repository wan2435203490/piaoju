/**
 * VITE_MOCK=1 时的内存 mock 实现（UI agent 开发一律走这里，不依赖后端起服）。
 * - 数据种子：./fixtures/*.json；写操作只改内存，刷新页面即还原
 * - 每个方法模拟 150ms 网络延迟
 * - 行为对齐契约：幂等 upsert、cursor 分页、建票联动建交易、404 → ApiError(40401)
 *
 * ⚠️ 本模块顶层禁止任何副作用（状态全部惰性初始化）：
 * 生产构建（VITE_MOCK 未开）依赖此特性把 mock + fixtures 整体摇树出包。
 */
import {
	ApiError,
	ERR,
	type Attachment,
	type AuthData,
	type Category,
	type CategoryInput,
	type ListPage,
	type LoginInput,
	type MonthlyStats,
	type RefreshData,
	type RegisterInput,
	type SyncChange,
	type SyncPushData,
	type SyncPullData,
	type Ticket,
	type TicketInput,
	type TicketQuery,
	type TicketStats,
	type TicketTransactionSummary,
	type Transaction,
	type TransactionInput,
	type TransactionQuery,
	type User
} from './types';
import {
	FIXTURE_MONTH,
	FIXTURE_YEAR,
	fixtureCategories,
	fixtureStatsMonthly,
	fixtureStatsTickets,
	fixtureTickets,
	fixtureTransactions,
	fixtureUser
} from './fixtures';
import { tokenStore } from './tokens';
import type { ApiClient } from './client';

const DELAY_MS = 150;
const delay = () => new Promise<void>((resolve) => setTimeout(resolve, DELAY_MS));
const clone = <T>(value: T): T => structuredClone(value);
const nowIso = () => new Date().toISOString();

/* ---------- 内存态（种子来自 fixtures，惰性初始化） ---------- */

interface MockDb {
	user: User;
	categories: Category[];
	transactions: Transaction[];
	tickets: Ticket[];
	categorySeq: number;
	attachmentSeq: number;
	/** mock 上传过的附件，供 createTicket 的 attachmentIds 引用 */
	uploaded: Attachment[];
}

let _db: MockDb | undefined;

function getDb(): MockDb {
	_db ??= {
		user: clone(fixtureUser),
		categories: clone(fixtureCategories),
		transactions: clone(fixtureTransactions),
		tickets: clone(fixtureTickets),
		categorySeq: 100,
		attachmentSeq: 1,
		uploaded: []
	};
	return _db;
}

/** 占位图（内联 SVG data URI，mock 模式不发外部图片请求；惰性生成） */
let _placeholder: string | undefined;

function placeholderImg(): string {
	_placeholder ??=
		'data:image/svg+xml;utf8,' +
		encodeURIComponent(
			'<svg xmlns="http://www.w3.org/2000/svg" width="480" height="270"><rect width="480" height="270" fill="#e7e0d3"/><text x="240" y="150" font-size="64" text-anchor="middle">🎫</text></svg>'
		);
	return _placeholder;
}

function issueTokens(): { accessToken: string; refreshToken: string } {
	const stamp = Date.now().toString(36);
	const pair = { accessToken: `mock-access-${stamp}`, refreshToken: `mock-refresh-${stamp}` };
	tokenStore.set(pair.accessToken, pair.refreshToken);
	return pair;
}

/** offset 型游标（真实后端为 opaque cursor，形状一致即可） */
function paginate<T>(items: T[], cursor: string | undefined, limit: number): ListPage<T> {
	const start = cursor ? Math.max(0, Number.parseInt(cursor, 10) || 0) : 0;
	const page = items.slice(start, start + limit);
	const nextCursor = start + limit < items.length ? String(start + limit) : null;
	return { items: clone(page), nextCursor };
}

const byOccurredDesc = (a: Transaction, b: Transaction) => (a.occurredAt < b.occurredAt ? 1 : -1);
const byEventDesc = (a: Ticket, b: Ticket) => (a.eventTime < b.eventTime ? 1 : -1);

function mustFindTicket(id: string): Ticket {
	const ticket = getDb().tickets.find((t) => t.id === id);
	if (!ticket) throw new ApiError(ERR.NOT_FOUND, '资源不存在或无权访问');
	return ticket;
}

/* ---------- ApiClient 的 mock 实现 ---------- */

export const mockApi: ApiClient = {
	/* ----- Auth：任意邮箱/密码都放行 ----- */
	async register(input: RegisterInput): Promise<AuthData> {
		await delay();
		const db = getDb();
		db.user = { ...db.user, email: input.email, nickname: input.nickname };
		return { user: clone(db.user), ...issueTokens() };
	},

	async login(input: LoginInput): Promise<AuthData> {
		await delay();
		const db = getDb();
		db.user = { ...db.user, email: input.email };
		return { user: clone(db.user), ...issueTokens() };
	},

	async refresh(): Promise<RefreshData> {
		await delay();
		return issueTokens();
	},

	async logout(): Promise<null> {
		await delay();
		tokenStore.clear();
		return null;
	},

	/* ----- Categories ----- */
	async listCategories(): Promise<Category[]> {
		await delay();
		return clone(getDb().categories);
	},

	async createCategory(input: CategoryInput): Promise<Category> {
		await delay();
		const db = getDb();
		const maxSort = Math.max(0, ...db.categories.filter((c) => c.kind === input.kind).map((c) => c.sort));
		const category: Category = { id: db.categorySeq++, ...input, isSystem: false, sort: maxSort + 1 };
		db.categories.push(category);
		return clone(category);
	},

	async updateCategory(id: number, patch: Partial<CategoryInput>): Promise<Category> {
		await delay();
		const category = getDb().categories.find((c) => c.id === id);
		if (!category) throw new ApiError(ERR.NOT_FOUND, '资源不存在或无权访问');
		if (category.isSystem) throw new ApiError(ERR.VALIDATION, '系统分类不可修改');
		Object.assign(category, patch);
		return clone(category);
	},

	async deleteCategory(id: number): Promise<null> {
		await delay();
		const db = getDb();
		const category = db.categories.find((c) => c.id === id);
		if (!category) throw new ApiError(ERR.NOT_FOUND, '资源不存在或无权访问');
		if (category.isSystem) throw new ApiError(ERR.VALIDATION, '系统分类不可删除');
		db.categories = db.categories.filter((c) => c.id !== id);
		// 契约 §3：删除后其交易归入「其他」
		const fallback = category.kind === 'income' ? 11 : 8;
		for (const tx of db.transactions) {
			if (tx.categoryId === id) tx.categoryId = fallback;
		}
		return null;
	},

	/* ----- Transactions ----- */
	async listTransactions(query: TransactionQuery = {}): Promise<ListPage<Transaction>> {
		await delay();
		let items = [...getDb().transactions];
		if (query.month) items = items.filter((t) => t.occurredAt.startsWith(query.month!));
		if (query.categoryId != null) items = items.filter((t) => t.categoryId === query.categoryId);
		if (query.direction) items = items.filter((t) => t.direction === query.direction);
		items.sort(byOccurredDesc);
		return paginate(items, query.cursor, query.limit ?? 50);
	},

	async createTransaction(input: TransactionInput): Promise<Transaction> {
		await delay();
		const db = getDb();
		const now = nowIso();
		const existing = db.transactions.find((t) => t.id === input.id);
		if (existing) {
			// 幂等 upsert：同 id 覆盖
			Object.assign(existing, input, { updatedAt: now });
			return clone(existing);
		}
		const tx: Transaction = { ...input, ticketId: null, createdAt: now, updatedAt: now };
		db.transactions.unshift(tx);
		return clone(tx);
	},

	async updateTransaction(id: string, patch: Partial<TransactionInput>): Promise<Transaction> {
		await delay();
		const tx = getDb().transactions.find((t) => t.id === id);
		if (!tx) throw new ApiError(ERR.NOT_FOUND, '资源不存在或无权访问');
		Object.assign(tx, patch, { id, updatedAt: nowIso() });
		return clone(tx);
	},

	async deleteTransaction(id: string): Promise<null> {
		await delay();
		const db = getDb();
		db.transactions = db.transactions.filter((t) => t.id !== id);
		return null;
	},

	/* ----- Tickets ----- */
	async listTickets(query: TicketQuery = {}): Promise<ListPage<Ticket>> {
		await delay();
		let items = [...getDb().tickets];
		if (query.kind) items = items.filter((t) => t.kind === query.kind);
		if (query.year != null) items = items.filter((t) => t.eventTime.startsWith(String(query.year)));
		items.sort(byEventDesc);
		return paginate(items, query.cursor, query.limit ?? 20);
	},

	async getTicket(id: string): Promise<Ticket> {
		await delay();
		return clone(mustFindTicket(id));
	},

	async createTicket(input: TicketInput): Promise<Ticket> {
		await delay();
		const db = getDb();
		const now = nowIso();
		const summary: TicketTransactionSummary = {
			id: crypto.randomUUID(),
			amountCents: input.amountCents,
			categoryId: input.categoryId,
			paymentMethod: input.paymentMethod
		};
		// 契约 §5：服务端事务内同时建 Transaction
		const tx: Transaction = {
			id: summary.id,
			amountCents: input.amountCents,
			direction: 'expense',
			categoryId: input.categoryId,
			note: input.title,
			occurredAt: input.occurredAt,
			paymentMethod: input.paymentMethod,
			ticketId: input.id,
			createdAt: now,
			updatedAt: now
		};
		const ticket: Ticket = {
			id: input.id,
			kind: input.kind,
			title: input.title,
			venue: input.venue,
			eventTime: input.eventTime,
			seat: input.seat,
			extra: input.extra,
			rating: input.rating,
			memo: input.memo,
			transaction: summary,
			attachments: db.uploaded.filter((a) => input.attachmentIds.includes(a.id)),
			createdAt: now,
			updatedAt: now
		};
		db.transactions.unshift(tx);
		db.tickets.unshift(ticket);
		return clone(ticket);
	},

	async updateTicket(id: string, patch: Partial<TicketInput>): Promise<Ticket> {
		await delay();
		const db = getDb();
		const ticket = mustFindTicket(id);
		const now = nowIso();
		const { amountCents, categoryId, paymentMethod, occurredAt, attachmentIds, ...rest } = patch;
		Object.assign(ticket, rest, { id, updatedAt: now });
		if (attachmentIds) {
			ticket.attachments = db.uploaded.filter((a) => attachmentIds.includes(a.id));
		}
		// 契约 §5：amountCents 等变更同步改关联交易
		if (amountCents != null) ticket.transaction.amountCents = amountCents;
		if (categoryId != null) ticket.transaction.categoryId = categoryId;
		if (paymentMethod) ticket.transaction.paymentMethod = paymentMethod;
		const tx = db.transactions.find((t) => t.id === ticket.transaction.id);
		if (tx) {
			if (amountCents != null) tx.amountCents = amountCents;
			if (categoryId != null) tx.categoryId = categoryId;
			if (paymentMethod) tx.paymentMethod = paymentMethod;
			if (occurredAt) tx.occurredAt = occurredAt;
			if (patch.title) tx.note = patch.title;
			tx.updatedAt = now;
		}
		return clone(ticket);
	},

	async deleteTicket(id: string): Promise<null> {
		await delay();
		const db = getDb();
		const ticket = mustFindTicket(id);
		// 契约 §5：软删票 + 关联交易
		db.tickets = db.tickets.filter((t) => t.id !== id);
		db.transactions = db.transactions.filter((t) => t.id !== ticket.transaction.id);
		return null;
	},

	/* ----- Uploads ----- */
	async upload(_file: File | Blob): Promise<Attachment> {
		await delay();
		const db = getDb();
		const attachment: Attachment = {
			id: db.attachmentSeq++,
			url: placeholderImg(),
			thumbUrl: placeholderImg(),
			w: 480,
			h: 270
		};
		db.uploaded.push(attachment);
		return clone(attachment);
	},

	/* ----- Stats：fixtures 月份/年份返回预生成数据，其余返回空 ----- */
	async statsMonthly(month: string): Promise<MonthlyStats> {
		await delay();
		if (month === FIXTURE_MONTH) return clone(fixtureStatsMonthly);
		return { expenseCents: 0, incomeCents: 0, byCategory: [], byDay: [] };
	},

	async statsTickets(year: number): Promise<TicketStats> {
		await delay();
		if (year === FIXTURE_YEAR) return clone(fixtureStatsTickets);
		return { total: 0, byKind: [] };
	},

	/* ----- Sync（M3 前的最小占位：push 全 applied，pull 全量无墓碑） ----- */
	async syncPush(changes: SyncChange[]): Promise<SyncPushData> {
		await delay();
		return {
			results: changes.map((c) => ({
				id: 'id' in c.payload ? c.payload.id : '',
				status: 'applied' as const,
				code: ERR.OK
			}))
		};
	},

	async syncPull(_since: string, _limit?: number): Promise<SyncPullData> {
		await delay();
		const db = getDb();
		return {
			transactions: clone(db.transactions).map((t) => ({ ...t, deletedAt: null })),
			tickets: clone(db.tickets).map((t) => ({ ...t, deletedAt: null })),
			categories: clone(db.categories).map((c) => ({ ...c, deletedAt: null })),
			nextCursor: '0',
			hasMore: false
		};
	}
};
