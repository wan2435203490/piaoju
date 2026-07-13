/**
 * outbox —— 全部写操作的统一入口（conventions §4「离线预留」）。
 *
 * 两种实现，调用方无感知（页面照旧 `import { outbox } from '$lib/db/outbox'`）：
 *
 * - **在线直写**（VITE_MOCK=1，或未 initOffline 时）：透传 $lib/api/client。
 * - **离线队列**（登录后 initOffline 建立本地库）：写本地 Dexie（乐观，标 _pending）
 *   → 入 outbox 队列 → kick 触发推送。返回本地实体，不等网络——离线也秒回。
 *
 * Dexie 与整个 db 层走动态 import 惰性加载：mock 模式与首屏包里不含 IndexedDB 代码
 * （包体红线 conventions §5：首屏 JS gzip < 200KB）。
 *
 * id 由调用方生成（$lib/utils/uuid）：transactions.id、tickets.id、
 * TicketInput.transactionId 全是客户端 UUID —— 离线创建不冲突 + 服务端幂等 upsert。
 *
 *   ✅ 页面里写数据：import { outbox } from '$lib/db/outbox'
 *   ❌ 页面里直接调 api.createTransaction 等写方法
 */
import { api } from '$lib/api/client';
import type {
	Attachment,
	Ticket,
	TicketInput,
	Transaction,
	TransactionInput
} from '$lib/api/types';

/** 写操作接口 —— 在线实现与离线实现共用 */
export interface Outbox {
	createTransaction(input: TransactionInput): Promise<Transaction>;
	updateTransaction(id: string, patch: Partial<TransactionInput>): Promise<Transaction>;
	deleteTransaction(id: string): Promise<null>;

	createTicket(input: TicketInput): Promise<Ticket>;
	updateTicket(id: string, patch: Partial<TicketInput>): Promise<Ticket>;
	deleteTicket(id: string): Promise<null>;

	/** 票面照片上传（离线时入队本地 blob，返回负数临时 id 占位） */
	upload(file: File | Blob): Promise<Attachment>;
}

/** 在线直写：透传 client（mock 模式、未登录、IndexedDB 不可用时） */
const passthrough: Outbox = {
	createTransaction: (input) => api.createTransaction(input),
	updateTransaction: (id, patch) => api.updateTransaction(id, patch),
	deleteTransaction: (id) => api.deleteTransaction(id),

	createTicket: (input) => api.createTicket(input),
	updateTicket: (id, patch) => api.updateTicket(id, patch),
	deleteTicket: (id) => api.deleteTicket(id),

	upload: (file) => api.upload(file)
};

/** 离线实现（initOffline 成功后装入；否则恒为 null → 走 passthrough） */
let offline: Outbox | null = null;

/** 是否已切到离线队列（UI 据此决定要不要显示「待同步」标记） */
export function isOfflineReady(): boolean {
	return offline !== null;
}

/**
 * 登录后调用：打开该用户的本地库、切到离线队列实现、启动同步调度。
 * mock 模式下 no-op（UI 开发不依赖 IndexedDB，conventions §4）。
 * 失败（浏览器禁用 IndexedDB、隐私模式等）→ 静默回退在线直写，功能不残废。
 */
export async function initOffline(userId: number): Promise<void> {
	if (import.meta.env.VITE_MOCK === '1' || typeof indexedDB === 'undefined') return;
	try {
		const [{ createOfflineOutbox }, { startSync }] = await Promise.all([
			import('./offline-outbox'),
			import('./sync-engine')
		]);
		offline = createOfflineOutbox(userId);
		startSync();
	} catch (err) {
		console.warn('[piaoju] 离线库不可用，回退在线直写', err);
		offline = null;
	}
}

/** 登出：停调度、关本地库、切回在线直写 */
export async function teardownOffline(): Promise<void> {
	if (!offline) return;
	offline = null;
	const [{ stopSync }, { closeDB }] = await Promise.all([
		import('./sync-engine'),
		import('./schema')
	]);
	stopSync();
	closeDB();
}

/** 唯一写入口。按当前是否已建立离线库分派。 */
export const outbox: Outbox = {
	createTransaction: (input) => (offline ?? passthrough).createTransaction(input),
	updateTransaction: (id, patch) => (offline ?? passthrough).updateTransaction(id, patch),
	deleteTransaction: (id) => (offline ?? passthrough).deleteTransaction(id),

	createTicket: (input) => (offline ?? passthrough).createTicket(input),
	updateTicket: (id, patch) => (offline ?? passthrough).updateTicket(id, patch),
	deleteTicket: (id) => (offline ?? passthrough).deleteTicket(id),

	upload: (file) => (offline ?? passthrough).upload(file)
};
