/**
 * 统一 API client —— 全项目唯一 fetch 出口（组件内禁止裸 fetch，conventions §4）。
 *
 * - 响应信封解包：code !== 0 → 抛 ApiError{code,message}
 * - 40101（access 过期）→ 自动 POST /auth/refresh 后原请求重试一次；
 *   refresh 失败清空本地 token 并抛错（页面层决定跳登录）
 * - token 持久化 localStorage：piaoju.access / piaoju.refresh（见 ./tokens.ts）
 * - VITE_MOCK=1 → 全部方法走 ./mock.ts（fixtures + 150ms 延迟），构建时死代码消除
 *
 * 读操作直接用 api.*；transactions/tickets 的写操作请一律走
 * $lib/db/outbox.ts（M3 换离线队列时调用方无感知）。
 */
import {
	ApiError,
	ERR,
	type Attachment,
	type AuthData,
	type CategoriesData,
	type Category,
	type CategoryInput,
	type Envelope,
	type ImportPreviewData,
	type ImportSource,
	type ListPage,
	type LoginInput,
	type MonthlyStats,
	type RefreshData,
	type RegisterInput,
	type SyncChange,
	type SyncPushData,
	type SyncPullData,
	type Ticket,
	type TicketDraft,
	type TicketInput,
	type TicketQuery,
	type TicketStats,
	type Transaction,
	type TransactionInput,
	type TransactionQuery
} from './types';
import { tokenStore, TOKEN_KEY } from './tokens';
import { mockApi } from './mock';

export { ApiError } from './types';
export { tokenStore, TOKEN_KEY } from './tokens';

const API_BASE: string = import.meta.env.VITE_API_BASE ?? '/api/v1';
const IS_MOCK: boolean = import.meta.env.VITE_MOCK === '1';

/* ============ 底层请求 ============ */

interface ReqOptions {
	/** JSON body（与 form 互斥） */
	body?: unknown;
	/** multipart 上传 */
	form?: FormData;
	/** query string（undefined 项自动剔除） */
	query?: Record<string, string | number | undefined>;
	/** false = 不带 Authorization（auth 接口自身） */
	auth?: boolean;
}

function buildUrl(path: string, query?: ReqOptions['query']): string {
	const usp = new URLSearchParams();
	for (const [key, value] of Object.entries(query ?? {})) {
		if (value !== undefined && value !== '') usp.set(key, String(value));
	}
	const qs = usp.toString();
	return `${API_BASE}${path}${qs ? `?${qs}` : ''}`;
}

async function exec<T>(method: string, url: string, opt: ReqOptions): Promise<T> {
	const headers = new Headers();
	const access = tokenStore.access;
	if (opt.auth !== false && access) headers.set('authorization', `Bearer ${access}`);

	let body: BodyInit | undefined;
	if (opt.form) {
		body = opt.form;
	} else if (opt.body !== undefined) {
		headers.set('content-type', 'application/json');
		body = JSON.stringify(opt.body);
	}

	let res: Response;
	try {
		res = await fetch(url, { method, headers, body });
	} catch {
		throw new ApiError(ERR.INTERNAL, '网络异常，请稍后重试');
	}

	let envelope: Envelope<T>;
	try {
		envelope = (await res.json()) as Envelope<T>;
	} catch {
		throw new ApiError(ERR.INTERNAL, `响应解析失败（HTTP ${res.status}）`);
	}

	if (envelope.code !== ERR.OK) throw new ApiError(envelope.code, envelope.message);
	return envelope.data;
}

/* ============ refresh 单飞（并发 401 只触发一次刷新） ============ */

let refreshing: Promise<RefreshData> | null = null;

function doRefresh(): Promise<RefreshData> {
	refreshing ??= (async () => {
		const refreshToken = tokenStore.refresh;
		if (!refreshToken) throw new ApiError(ERR.REFRESH_INVALID, '未登录');
		try {
			const data = await exec<RefreshData>('POST', buildUrl('/auth/refresh'), {
				body: { refreshToken },
				auth: false
			});
			// 旋转：旧 refresh 已被服务端吊销，立即换存新对
			tokenStore.set(data.accessToken, data.refreshToken);
			return data;
		} catch (error) {
			tokenStore.clear();
			throw error;
		} finally {
			refreshing = null;
		}
	})();
	return refreshing;
}

async function request<T>(method: string, path: string, opt: ReqOptions = {}): Promise<T> {
	const url = buildUrl(path, opt.query);
	try {
		return await exec<T>(method, url, opt);
	} catch (error) {
		if (error instanceof ApiError && error.code === ERR.TOKEN_EXPIRED) {
			await doRefresh();
			return exec<T>(method, url, opt); // 只重试一次
		}
		throw error;
	}
}

/* ============ 契约方法集（PROTOCOL §2-§8 全覆盖） ============ */

export interface ApiClient {
	/** POST /auth/register；成功后已持久化 token */
	register(input: RegisterInput): Promise<AuthData>;
	/** POST /auth/login；成功后已持久化 token */
	login(input: LoginInput): Promise<AuthData>;
	/** POST /auth/refresh（自动刷新已内置，一般无需手调） */
	refresh(): Promise<RefreshData>;
	/** POST /auth/logout；无论成败都清空本地 token */
	logout(): Promise<null>;

	/** GET /categories（系统 + 本人自定义；已解包 items） */
	listCategories(): Promise<Category[]>;
	/** POST /categories */
	createCategory(input: CategoryInput): Promise<Category>;
	/** PATCH /categories/{id}（仅自定义分类；契约 §3 不允许改 kind，服务端只接受 name/icon） */
	updateCategory(id: number, patch: Partial<Pick<CategoryInput, 'name' | 'icon'>>): Promise<Category>;
	/** DELETE /categories/{id}（其交易归入「其他」） */
	deleteCategory(id: number): Promise<null>;

	/** GET /transactions（occurredAt desc，cursor 分页） */
	listTransactions(query?: TransactionQuery): Promise<ListPage<Transaction>>;
	/** POST /transactions（id 客户端 UUID，幂等 upsert）——写操作请走 outbox */
	createTransaction(input: TransactionInput): Promise<Transaction>;
	/** PATCH /transactions/{id} ——写操作请走 outbox */
	updateTransaction(id: string, patch: Partial<TransactionInput>): Promise<Transaction>;
	/** DELETE /transactions/{id}（软删）——写操作请走 outbox */
	deleteTransaction(id: string): Promise<null>;

	/** GET /tickets（eventTime desc，cursor 分页） */
	listTickets(query?: TicketQuery): Promise<ListPage<Ticket>>;
	/** GET /tickets/{id}；不存在 → ApiError(40401) */
	getTicket(id: string): Promise<Ticket>;
	/** POST /tickets（服务端事务内同时建 Transaction）——写操作请走 outbox */
	createTicket(input: TicketInput): Promise<Ticket>;
	/** PATCH /tickets/{id}（amountCents 变更同步改交易）——写操作请走 outbox */
	updateTicket(id: string, patch: Partial<TicketInput>): Promise<Ticket>;
	/** DELETE /tickets/{id}（软删票 + 关联交易）——写操作请走 outbox */
	deleteTicket(id: string): Promise<null>;

	/** POST /uploads（jpeg/png/webp/heic ≤10MB） */
	upload(file: File | Blob): Promise<Attachment>;

	/**
	 * POST /tickets/recognize（§6.1）：已上传的票面照 → 票据草稿（服务端不落库）。
	 * 50001 = 识票服务未配置（功能与主流程解耦，UI 应隐藏入口）；42901 = 限流。
	 */
	recognizeTicket(attachmentId: number): Promise<TicketDraft>;

	/**
	 * POST /imports/preview（§6.2）：账单 CSV → 解析 + 规则分类 + 查重。
	 * 只有 preview 没有 commit —— 写入一律走 outbox（离线安全 + 幂等）。
	 * 40001 = 不是该来源的账单格式；41301 = 文件 >5MB。
	 */
	previewImport(file: File | Blob, source: ImportSource): Promise<ImportPreviewData>;

	/** GET /stats/monthly?month=2026-07 */
	statsMonthly(month: string): Promise<MonthlyStats>;
	/** GET /stats/tickets?year=2026 */
	statsTickets(year: number): Promise<TicketStats>;

	/** POST /sync/push（M3） */
	syncPush(changes: SyncChange[]): Promise<SyncPushData>;
	/** GET /sync/pull?since=&limit=（M3） */
	syncPull(since: string, limit?: number): Promise<SyncPullData>;
}

const httpApi: ApiClient = {
	async register(input) {
		const data = await request<AuthData>('POST', '/auth/register', { body: input, auth: false });
		tokenStore.set(data.accessToken, data.refreshToken);
		return data;
	},

	async login(input) {
		const data = await request<AuthData>('POST', '/auth/login', { body: input, auth: false });
		tokenStore.set(data.accessToken, data.refreshToken);
		return data;
	},

	refresh: () => doRefresh(),

	async logout() {
		const refreshToken = tokenStore.refresh;
		try {
			if (refreshToken) await request<null>('POST', '/auth/logout', { body: { refreshToken } });
		} finally {
			tokenStore.clear();
		}
		return null;
	},

	listCategories: () => request<CategoriesData>('GET', '/categories').then((d) => d.items),
	createCategory: (input) => request<Category>('POST', '/categories', { body: input }),
	updateCategory: (id, patch) => request<Category>('PATCH', `/categories/${id}`, { body: patch }),
	deleteCategory: (id) => request<null>('DELETE', `/categories/${id}`),

	listTransactions: (query = {}) =>
		request<ListPage<Transaction>>('GET', '/transactions', {
			query: {
				month: query.month,
				categoryId: query.categoryId,
				direction: query.direction,
				cursor: query.cursor,
				limit: query.limit
			}
		}),
	createTransaction: (input) => request<Transaction>('POST', '/transactions', { body: input }),
	updateTransaction: (id, patch) =>
		request<Transaction>('PATCH', `/transactions/${id}`, { body: patch }),
	deleteTransaction: (id) => request<null>('DELETE', `/transactions/${id}`),

	listTickets: (query = {}) =>
		request<ListPage<Ticket>>('GET', '/tickets', {
			query: { kind: query.kind, year: query.year, cursor: query.cursor, limit: query.limit }
		}),
	getTicket: (id) => request<Ticket>('GET', `/tickets/${id}`),
	createTicket: (input) => request<Ticket>('POST', '/tickets', { body: input }),
	updateTicket: (id, patch) => request<Ticket>('PATCH', `/tickets/${id}`, { body: patch }),
	deleteTicket: (id) => request<null>('DELETE', `/tickets/${id}`),

	upload: (file) => {
		const form = new FormData();
		form.append('file', file);
		return request<Attachment>('POST', '/uploads', { form });
	},

	recognizeTicket: (attachmentId) =>
		request<TicketDraft>('POST', '/tickets/recognize', { body: { attachmentId } }),

	previewImport: (file, source) => {
		const form = new FormData();
		form.append('file', file);
		form.append('source', source);
		return request<ImportPreviewData>('POST', '/imports/preview', { form });
	},

	statsMonthly: (month) => request<MonthlyStats>('GET', '/stats/monthly', { query: { month } }),
	statsTickets: (year) => request<TicketStats>('GET', '/stats/tickets', { query: { year } }),

	syncPush: (changes) => request<SyncPushData>('POST', '/sync/push', { body: { changes } }),
	syncPull: (since, limit) => request<SyncPullData>('GET', '/sync/pull', { query: { since, limit } })
};

/** 唯一 API 入口。VITE_MOCK=1 → fixtures mock；否则 → 真实后端 */
export const api: ApiClient = IS_MOCK ? mockApi : httpApi;
