/**
 * Dexie 本地库（W5 离线引擎）—— 契约 §8 的客户端侧。
 *
 * 表设计
 * - transactions / tickets / categories：服务端实体的本地镜像。含软删墓碑
 *   （deletedAt 非 null 的行保留，读查询过滤）——本地立刻删行会被下一次 pull
 *   的旧版本复活，故必须留墓碑。
 * - outbox：待推送的写操作队列（FIFO，seq 自增）。指数退避字段 attempts /
 *   nextAttemptAt 由 sync-engine 维护。
 * - blobs：离线拍的票面照片。在线时才换得到服务端 attachment id，故离线期用
 *   负数临时 id 占位（见 outbox.ts 的 upload），flush 时替换为真 id。
 * - meta：kv 杂项，目前只存 pull 游标（key='syncCursor'）。
 *
 * 本地元字段（下划线前缀，不进 API payload）
 * - _pending: 1 = 有未推送的本地改动。pull 合并时不被服务端版本覆盖（本地优先，
 *   等 push 定胜负）；UI 据此打「待同步」标记。
 *
 * 全库按登录用户隔离：库名带 userId（换号即换库，不串数据）。
 */
import Dexie, { type Table } from 'dexie';
import type { Category, Ticket, Transaction } from '$lib/api/types';

/** 本地行 = 服务端实体 + 墓碑 + 待同步标记 */
export type Local<T> = T & {
	deletedAt?: string | null;
	/** 1 = 有未推送改动（Dexie 不索引 boolean，故用 0/1） */
	_pending?: 0 | 1;
};

export type LocalTransaction = Local<Transaction>;
export type LocalCategory = Local<Category>;

/**
 * 本地票行额外冗余 occurredAt：契约 §5 的 Ticket 响应实体里没有这个字段（它在联动
 * 交易上），但 push 的 TicketInput 需要它。离线重推时不能指望联动交易行还在，故随票存一份。
 */
export type LocalTicket = Local<Ticket> & { occurredAt?: string };

/** outbox 队列项：一条待推送的写操作 */
export interface OutboxItem {
	seq?: number;
	entity: 'transaction' | 'ticket';
	op: 'upsert' | 'delete';
	/** 业务主键（客户端 UUID）——重试时据此覆盖同实体的旧队列项 */
	id: string;
	/** 契约 §8 的 payload：TransactionInput | TicketInput | { id } */
	payload: Record<string, unknown>;
	/** 契约 §8 LWW 依据（RFC3339 UTC） */
	clientUpdatedAt: string;
	attempts: number;
	/** 下次可尝试时间（epoch ms）；退避期内 flush 跳过 */
	nextAttemptAt: number;
	lastError?: string;
}

/** 离线拍的照片：等在线时上传换真 attachment id */
export interface BlobItem {
	/** 自增正数；对外暴露为负数临时 attachment id（-id） */
	id?: number;
	blob: Blob;
	createdAt: string;
}

export interface MetaItem {
	key: string;
	value: string;
}

export class PiaojuDB extends Dexie {
	transactions!: Table<LocalTransaction, string>;
	tickets!: Table<LocalTicket, string>;
	categories!: Table<LocalCategory, number>;
	outbox!: Table<OutboxItem, number>;
	blobs!: Table<BlobItem, number>;
	meta!: Table<MetaItem, string>;

	constructor(userId: number) {
		super(`piaoju-${userId}`);
		this.version(1).stores({
			// occurredAt/eventTime 供列表排序，updatedAt 供 LWW，_pending 供待同步筛选
			transactions: 'id, occurredAt, updatedAt, deletedAt, _pending',
			tickets: 'id, eventTime, kind, updatedAt, deletedAt, _pending',
			categories: 'id, kind, deletedAt',
			outbox: '++seq, id, nextAttemptAt',
			blobs: '++id',
			meta: 'key'
		});
	}
}

/** 当前库实例（登录后由 openDB 建立；登出置空） */
let db: PiaojuDB | null = null;
let dbUserId: number | null = null;

/** 打开（或切换到）某用户的本地库。同一 userId 重复调用复用实例。 */
export function openDB(userId: number): PiaojuDB {
	if (db && dbUserId === userId) return db;
	db?.close();
	db = new PiaojuDB(userId);
	dbUserId = userId;
	return db;
}

/** 取当前库；未登录/未打开时返回 null（调用方需回退在线直写） */
export function currentDB(): PiaojuDB | null {
	return db;
}

/** 登出：关闭句柄（不删数据——同一用户再登录可复用本地缓存） */
export function closeDB(): void {
	db?.close();
	db = null;
	dbUserId = null;
}

/** 换号或「清除本地数据」：彻底删库 */
export async function destroyDB(userId: number): Promise<void> {
	if (dbUserId === userId) closeDB();
	await Dexie.delete(`piaoju-${userId}`);
}

/* ============ meta：pull 游标 ============ */

const CURSOR_KEY = 'syncCursor';

export async function getCursor(d: PiaojuDB): Promise<string> {
	return (await d.meta.get(CURSOR_KEY))?.value ?? '';
}

export async function setCursor(d: PiaojuDB, cursor: string): Promise<void> {
	await d.meta.put({ key: CURSOR_KEY, value: cursor });
}
