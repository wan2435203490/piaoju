/**
 * outbox —— 全部写操作的统一入口（conventions §4「离线预留」）。
 *
 * M1/M2 当前实现：直接透传 $lib/api/client（在线直写）。
 * M3（W5）将把实现替换为 Dexie 本地队列 + 后台同步（重试/退避/在线探测），
 * 调用方（W2/W3/W4 的页面与组件）无感知 —— 所以：
 *
 *   ✅ 页面里写数据：import { outbox } from '$lib/db/outbox'
 *   ❌ 页面里直接调 api.createTransaction 等写方法
 *
 * id 由调用方生成（$lib/utils/uuid），保证离线创建不冲突 + 服务端幂等 upsert。
 */
import { api } from '$lib/api/client';
import type {
	Attachment,
	Ticket,
	TicketInput,
	Transaction,
	TransactionInput
} from '$lib/api/types';

/** 写操作接口 —— M3 的 Dexie 实现必须完整实现同一接口 */
export interface Outbox {
	createTransaction(input: TransactionInput): Promise<Transaction>;
	updateTransaction(id: string, patch: Partial<TransactionInput>): Promise<Transaction>;
	deleteTransaction(id: string): Promise<null>;

	createTicket(input: TicketInput): Promise<Ticket>;
	updateTicket(id: string, patch: Partial<TicketInput>): Promise<Ticket>;
	deleteTicket(id: string): Promise<null>;

	/** 票面照片上传（离线时 M3 会入队本地 blob） */
	upload(file: File | Blob): Promise<Attachment>;
}

/** M1/M2 实现：透传 client */
export const outbox: Outbox = {
	createTransaction: (input) => api.createTransaction(input),
	updateTransaction: (id, patch) => api.updateTransaction(id, patch),
	deleteTransaction: (id) => api.deleteTransaction(id),

	createTicket: (input) => api.createTicket(input),
	updateTicket: (id, patch) => api.updateTicket(id, patch),
	deleteTicket: (id) => api.deleteTicket(id),

	upload: (file) => api.upload(file)
};
