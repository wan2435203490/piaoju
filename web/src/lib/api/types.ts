/**
 * 拾光票局 API 类型 —— docs/PROTOCOL.md 的逐字段 TypeScript 对齐（唯一类型源）。
 * 契约变更时同步修改此文件；任何字段疑义以 PROTOCOL.md 为准并上报主线程。
 *
 * 通用规范（piaoju-conventions §1）：
 * - 金额一律整数「分」（amountCents），渲染层才除 100
 * - 时间一律 RFC3339 UTC 字符串（如 2026-07-12T11:30:00Z），展示层转本地时区
 * - transactions/tickets 业务主键为客户端生成 UUIDv4
 * - JSON 字段一律 camelCase
 */

/* ============ 响应信封（conventions §2） ============ */

export interface Envelope<T> {
	code: number;
	message: string;
	data: T;
}

/* ============ 错误码表（PROTOCOL §1） ============ */

export const ERR = {
	/** ok */
	OK: 0,
	/** 参数校验失败 */
	VALIDATION: 40001,
	/** 不支持的枚举值 */
	UNSUPPORTED_ENUM: 40002,
	/** access token 过期 */
	TOKEN_EXPIRED: 40101,
	/** refresh token 无效/已吊销 */
	REFRESH_INVALID: 40102,
	/** 邮箱或密码错误 */
	BAD_CREDENTIALS: 40103,
	/** 资源不存在或无权访问 */
	NOT_FOUND: 40401,
	/** 邮箱已注册 */
	EMAIL_TAKEN: 40901,
	/** 幂等冲突（同 id 不同内容且 updatedAt 更旧） */
	IDEMPOTENCY_CONFLICT: 40902,
	/** 上传文件超限（>10MB 或非图片；导入 CSV 为 >5MB） */
	UPLOAD_REJECTED: 41301,
	/** 识票调用超额/限流（PROTOCOL §6.1，v1.3） */
	RECOGNIZE_RATE_LIMITED: 42901,
	/** 服务端错误 */
	INTERNAL: 50000,
	/** 识票服务未配置（服务端未设 PIAOJU_LLM_API_KEY，PROTOCOL §6.1，v1.3） */
	RECOGNIZE_UNAVAILABLE: 50001
} as const;

export type ErrCode = (typeof ERR)[keyof typeof ERR];

/** 信封 code !== 0 时 client 抛出的错误 */
export class ApiError extends Error {
	readonly code: number;
	constructor(code: number, message: string) {
		super(message);
		this.name = 'ApiError';
		this.code = code;
	}
}

/* ============ 枚举（conventions §1） ============ */

export type Direction = 'expense' | 'income';

export type TicketKind = 'movie' | 'show' | 'attraction' | 'train' | 'flight' | 'other';

export type PaymentMethod = 'wechat' | 'alipay' | 'cash' | 'card' | 'other';

export const TICKET_KINDS = ['movie', 'show', 'attraction', 'train', 'flight', 'other'] as const satisfies readonly TicketKind[];

export const PAYMENT_METHODS = ['wechat', 'alipay', 'cash', 'card', 'other'] as const satisfies readonly PaymentMethod[];

/* ============ Auth（PROTOCOL §2） ============ */

export interface User {
	id: number;
	email: string;
	nickname: string;
	createdAt: string;
}

export interface RegisterInput {
	email: string;
	/** ≥ 8 位 */
	password: string;
	nickname: string;
}

export interface LoginInput {
	email: string;
	password: string;
}

/** POST /auth/register、POST /auth/login → data */
export interface AuthData {
	user: User;
	accessToken: string;
	refreshToken: string;
}

/** POST /auth/refresh → data（旋转：旧 refresh 立即吊销） */
export interface RefreshData {
	accessToken: string;
	refreshToken: string;
}

/* ============ Categories（PROTOCOL §3） ============ */

export interface Category {
	id: number;
	name: string;
	/** emoji */
	icon: string;
	kind: Direction;
	isSystem: boolean;
	sort: number;
}

/** POST /categories 请求体 */
export interface CategoryInput {
	name: string;
	icon: string;
	kind: Direction;
}

/** GET /categories → data */
export interface CategoriesData {
	items: Category[];
}

/* ============ 列表分页（conventions §2：?cursor=&limit=） ============ */

export interface ListPage<T> {
	items: T[];
	nextCursor: string | null;
}

/* ============ Transactions（PROTOCOL §4） ============ */

export interface Transaction {
	/** 客户端生成 UUIDv4 */
	id: string;
	amountCents: number;
	direction: Direction;
	categoryId: number;
	note: string;
	/** RFC3339 UTC */
	occurredAt: string;
	paymentMethod: PaymentMethod;
	/** 反向关联，只读；无关联为 null */
	ticketId: string | null;
	createdAt: string;
	updatedAt: string;
}

/** POST /transactions 请求体（= Transaction 去掉 ticketId/createdAt/updatedAt） */
export type TransactionInput = Omit<Transaction, 'ticketId' | 'createdAt' | 'updatedAt'>;

/** GET /transactions 查询参数 */
export interface TransactionQuery {
	/** 形如 2026-07 */
	month?: string;
	categoryId?: number;
	direction?: Direction;
	cursor?: string;
	/** 默认 50 */
	limit?: number;
}

/* ============ Tickets（PROTOCOL §5） ============ */

/** extra 按 kind（全部字段可空字符串） */
export interface MovieExtra {
	cinema: string;
	hall: string;
	/** IMAX / 杜比… */
	filmFormat: string;
}

export interface ShowExtra {
	tour: string;
	session: string;
	zone: string;
}

export interface AttractionExtra {
	city: string;
	/** 成人 / 学生… */
	ticketType: string;
}

export interface TrainExtra {
	trainNo: string;
	fromStation: string;
	toStation: string;
	departTime: string;
	arriveTime: string;
	seatClass: string;
}

export interface FlightExtra {
	flightNo: string;
	airline: string;
	fromAirport: string;
	toAirport: string;
	departTime: string;
	arriveTime: string;
	cabin: string;
}

export type OtherExtra = Record<string, never>;

export interface ExtraByKind {
	movie: MovieExtra;
	show: ShowExtra;
	attraction: AttractionExtra;
	train: TrainExtra;
	flight: FlightExtra;
	other: OtherExtra;
}

export type TicketExtra = ExtraByKind[TicketKind];

export interface Attachment {
	id: number;
	url: string;
	thumbUrl: string;
	w: number;
	h: number;
}

/** Ticket 内嵌的只读交易摘要 */
export interface TicketTransactionSummary {
	id: string;
	amountCents: number;
	categoryId: number;
	paymentMethod: PaymentMethod;
}

export interface Ticket<K extends TicketKind = TicketKind> {
	/** 客户端生成 UUIDv4 */
	id: string;
	kind: K;
	title: string;
	venue: string;
	/** RFC3339 UTC */
	eventTime: string;
	seat: string;
	extra: ExtraByKind[K];
	/** 0-5，0 = 未评 */
	rating: number;
	memo: string;
	transaction: TicketTransactionSummary;
	attachments: Attachment[];
	createdAt: string;
	updatedAt: string;
}

/** 按 kind 收窄 Ticket（含 extra 类型） */
export function isKind<K extends TicketKind>(ticket: Ticket, kind: K): ticket is Ticket<K> {
	return ticket.kind === kind;
}

/** POST /tickets 请求体（服务端事务内同时建 Transaction） */
export interface TicketInput<K extends TicketKind = TicketKind> {
	id: string;
	/**
	 * 联动交易的主键，与 id 一样由客户端生成 UUIDv4（契约 §5 v1.2）。
	 * 离线建票时本地要立刻把这笔交易写进账本，等不了服务端生成。
	 */
	transactionId: string;
	kind: K;
	title: string;
	venue: string;
	eventTime: string;
	seat: string;
	extra: ExtraByKind[K];
	rating: number;
	memo: string;
	amountCents: number;
	categoryId: number;
	paymentMethod: PaymentMethod;
	occurredAt: string;
	attachmentIds: number[];
}

/** GET /tickets 查询参数 */
export interface TicketQuery {
	kind?: TicketKind;
	/** 形如 2026 */
	year?: number;
	cursor?: string;
	/** 默认 20 */
	limit?: number;
}

/* ============ Stats（PROTOCOL §7） ============ */

export interface CategoryStat {
	categoryId: number;
	cents: number;
	count: number;
}

export interface DayStat {
	/** 形如 2026-07-01 */
	date: string;
	expenseCents: number;
}

/** GET /stats/monthly?month= → data */
export interface MonthlyStats {
	expenseCents: number;
	incomeCents: number;
	byCategory: CategoryStat[];
	byDay: DayStat[];
}

export interface KindStat {
	kind: TicketKind;
	count: number;
	cents: number;
}

/** GET /stats/tickets?year= → data */
export interface TicketStats {
	total: number;
	byKind: KindStat[];
}

/* ============ Sync（PROTOCOL §8，M3 启用；类型先行供 outbox/W5 使用） ============ */

export type SyncEntity = 'transaction' | 'ticket';

export type SyncOp = 'upsert' | 'delete';

export interface SyncChange {
	entity: SyncEntity;
	op: SyncOp;
	payload: TransactionInput | TicketInput | { id: string };
	clientUpdatedAt: string;
}

export interface SyncResult {
	id: string;
	status: 'applied' | 'stale' | 'error';
	code: number;
}

/** POST /sync/push → data */
export interface SyncPushData {
	results: SyncResult[];
}

/** pull 下发实体带软删墓碑 */
export type Tombstone<T> = T & { deletedAt: string | null };

/** GET /sync/pull?since=&limit= → data */
export interface SyncPullData {
	transactions: Tombstone<Transaction>[];
	tickets: Tombstone<Ticket>[];
	categories: Tombstone<Category>[];
	/** 服务端单调游标 */
	nextCursor: string;
	hasMore: boolean;
}

/* ============ 识票（PROTOCOL §6.1，v1.3） ============ */

/**
 * POST /tickets/recognize → data。
 * **草稿**，服务端不落库：客户端回填表单、用户确认后才走 §5 建票。
 * 识别不出的字段回零值（""/0/{}），不猜。
 */
export interface TicketDraft<K extends TicketKind = TicketKind> {
	kind: K;
	title: string;
	venue: string;
	/** RFC3339 UTC；识别不出为 '' */
	eventTime: string;
	seat: string;
	/** 按识别出的 kind 走 §5 的 extra 白名单字段 */
	extra: ExtraByKind[K];
	amountCents: number;
	/** 0-1；< 0.6 时客户端应提示「识别可能不准，请核对」 */
	confidence: number;
}

/** 识别结果可信度下限（低于此值提示用户核对，契约 §6.1） */
export const RECOGNIZE_CONFIDENCE_FLOOR = 0.6;

/* ============ 账单导入（PROTOCOL §6.2，v1.3） ============ */

export type ImportSource = 'wechat' | 'alipay';

export const IMPORT_SOURCES = ['wechat', 'alipay'] as const satisfies readonly ImportSource[];

/** POST /imports/preview 返回的单行（只有 preview，没有 commit：写入走 outbox） */
export interface ImportRow {
	rowIndex: number;
	amountCents: number;
	direction: Direction;
	/** RFC3339 UTC */
	occurredAt: string;
	note: string;
	paymentMethod: PaymentMethod;
	/** 规则匹配的建议分类 */
	categoryId: number;
	/** 与库中同金额 + 同时刻的交易撞上 → 建议跳过 */
	duplicate: boolean;
}

/** POST /imports/preview → data */
export interface ImportPreviewData {
	items: ImportRow[];
	total: number;
	duplicates: number;
}

/** 导入 CSV 体积上限（契约 §6.2：>5MB → 41301） */
export const IMPORT_MAX_BYTES = 5 * 1024 * 1024;
