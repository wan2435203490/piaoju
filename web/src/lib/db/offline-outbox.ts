/**
 * 离线队列实现（W5）—— outbox 接口的 Dexie 版。
 *
 * 每个写操作：写本地库（乐观，标 _pending=1）→ 入 outbox 队列 → kick 推送。
 * 立刻返回本地实体，不等网络：离线时用户照样秒见结果，联网后自动补推。
 *
 * 关键点
 * - **建票同时建联动交易**：契约 §5 v1.2 的 transactionId 由客户端生成，本地就能把
 *   这笔支出直接写进 transactions 表 —— 离线建的票，账本页/统计页立刻能看见。
 *   （服务端也用同一个 id 建交易，pull 回来是同一行，不会重复。）
 * - **队列合并**：同一 id 尚未尝试发送（attempts=0）的 upsert 会被新改动覆盖，
 *   连续编辑十次只推最后一次。已在飞行/退避中的项不动，交给服务端 LWW 定序。
 * - **离线照片**：blob 存本地，返回负数临时 attachment id 占位；flush 时换成真 id
 *   （见 sync-engine.resolveAttachments）。
 */
import { api } from '$lib/api/client';
import type {
	Attachment,
	Ticket,
	TicketInput,
	Transaction,
	TransactionInput
} from '$lib/api/types';
import type { Outbox } from './outbox';
import { nowIso } from './clock';
import {
	openDB,
	type LocalTicket,
	type LocalTransaction,
	type OutboxItem,
	type PiaojuDB
} from './schema';
import { isOnline, kick } from './sync-engine';

/** 入队：同 id 的未发送 upsert 直接覆盖（合并连续编辑），否则新增 */
async function enqueue(
	db: PiaojuDB,
	entity: OutboxItem['entity'],
	op: OutboxItem['op'],
	id: string,
	payload: Record<string, unknown>,
	clientUpdatedAt: string
): Promise<void> {
	await db.transaction('rw', db.outbox, async () => {
		if (op === 'upsert') {
			const mergeable = await db.outbox
				.where('id')
				.equals(id)
				.filter((it) => it.op === 'upsert' && it.attempts === 0)
				.first();
			if (mergeable) {
				await db.outbox.update(mergeable.seq!, { payload, clientUpdatedAt });
				return;
			}
		}
		await db.outbox.add({
			entity,
			op,
			id,
			payload,
			clientUpdatedAt,
			attempts: 0,
			nextAttemptAt: 0
		});
	});
	kick();
}

/** 本地行 → 契约 TransactionInput（push payload 要完整实体，不是 patch） */
function toTransactionInput(row: LocalTransaction): Record<string, unknown> {
	return {
		id: row.id,
		amountCents: row.amountCents,
		direction: row.direction,
		categoryId: row.categoryId,
		note: row.note,
		occurredAt: row.occurredAt,
		paymentMethod: row.paymentMethod
	};
}

/** 本地票行 → 契约 TicketInput */
function toTicketInput(row: LocalTicket, attachmentIds: number[]): Record<string, unknown> {
	return {
		id: row.id,
		transactionId: row.transaction.id,
		kind: row.kind,
		title: row.title,
		venue: row.venue,
		eventTime: row.eventTime,
		seat: row.seat,
		extra: row.extra,
		rating: row.rating,
		memo: row.memo,
		amountCents: row.transaction.amountCents,
		categoryId: row.transaction.categoryId,
		paymentMethod: row.transaction.paymentMethod,
		occurredAt: row.occurredAt ?? row.eventTime,
		attachmentIds
	};
}

export function createOfflineOutbox(userId: number): Outbox {
	const db = openDB(userId);

	return {
		async createTransaction(input: TransactionInput): Promise<Transaction> {
			const ts = nowIso();
			const row: LocalTransaction = {
				...input,
				ticketId: null,
				createdAt: ts,
				updatedAt: ts,
				deletedAt: null,
				_pending: 1
			};
			await db.transactions.put(row);
			await enqueue(db, 'transaction', 'upsert', input.id, toTransactionInput(row), ts);
			return row;
		},

		async updateTransaction(id: string, patch: Partial<TransactionInput>): Promise<Transaction> {
			const cur = await db.transactions.get(id);
			if (!cur) throw new Error(`transaction ${id} not found locally`);
			const ts = nowIso();
			const row: LocalTransaction = { ...cur, ...patch, updatedAt: ts, _pending: 1 };
			await db.transactions.put(row);
			await enqueue(db, 'transaction', 'upsert', id, toTransactionInput(row), ts);
			return row;
		},

		async deleteTransaction(id: string): Promise<null> {
			const ts = nowIso();
			// 墓碑而非删行：直接删会被下一次 pull 的旧快照复活
			await db.transactions.update(id, { deletedAt: ts, updatedAt: ts, _pending: 1 } as never);
			await enqueue(db, 'transaction', 'delete', id, { id }, ts);
			return null;
		},

		async createTicket(input: TicketInput): Promise<Ticket> {
			const ts = nowIso();
			const ticket: LocalTicket = {
				id: input.id,
				kind: input.kind,
				title: input.title,
				venue: input.venue,
				eventTime: input.eventTime,
				seat: input.seat,
				extra: input.extra,
				rating: input.rating,
				memo: input.memo,
				transaction: {
					id: input.transactionId,
					amountCents: input.amountCents,
					categoryId: input.categoryId,
					paymentMethod: input.paymentMethod
				},
				attachments: [],
				occurredAt: input.occurredAt,
				createdAt: ts,
				updatedAt: ts,
				deletedAt: null,
				_pending: 1
			};
			// 联动交易一并写本地（契约 §5：服务端事务内同时建 Transaction，note = title）。
			// 服务端会用同一个 transactionId 建同一行，pull 回来覆盖，不产生重复。
			const linked: LocalTransaction = {
				id: input.transactionId,
				amountCents: input.amountCents,
				direction: 'expense',
				categoryId: input.categoryId,
				note: input.title,
				occurredAt: input.occurredAt,
				paymentMethod: input.paymentMethod,
				ticketId: input.id,
				createdAt: ts,
				updatedAt: ts,
				deletedAt: null,
				_pending: 1 // 随票一起 push，本身不单独入队
			};
			await db.transaction('rw', db.tickets, db.transactions, async () => {
				await db.tickets.put(ticket);
				await db.transactions.put(linked);
			});
			await enqueue(db, 'ticket', 'upsert', input.id, toTicketInput(ticket, input.attachmentIds), ts);
			return ticket;
		},

		async updateTicket(id: string, patch: Partial<TicketInput>): Promise<Ticket> {
			const cur = (await db.tickets.get(id)) as LocalTicket | undefined;
			if (!cur) throw new Error(`ticket ${id} not found locally`);
			const ts = nowIso();

			const row: LocalTicket = {
				...cur,
				...('kind' in patch ? { kind: patch.kind! } : {}),
				...('title' in patch ? { title: patch.title! } : {}),
				...('venue' in patch ? { venue: patch.venue! } : {}),
				...('eventTime' in patch ? { eventTime: patch.eventTime! } : {}),
				...('seat' in patch ? { seat: patch.seat! } : {}),
				...('extra' in patch ? { extra: patch.extra! } : {}),
				...('rating' in patch ? { rating: patch.rating! } : {}),
				...('memo' in patch ? { memo: patch.memo! } : {}),
				...('occurredAt' in patch ? { occurredAt: patch.occurredAt! } : {}),
				transaction: {
					id: cur.transaction.id, // 联动交易主键不可变
					amountCents: patch.amountCents ?? cur.transaction.amountCents,
					categoryId: patch.categoryId ?? cur.transaction.categoryId,
					paymentMethod: patch.paymentMethod ?? cur.transaction.paymentMethod
				},
				updatedAt: ts,
				_pending: 1
			};
			const attachmentIds = patch.attachmentIds ?? row.attachments.map((a) => a.id);

			// 金额/分类/支付方式变更要同步改联动交易（契约 §5），本地也得同步，
			// 否则账本页显示的还是旧金额。
			await db.transaction('rw', db.tickets, db.transactions, async () => {
				await db.tickets.put(row);
				const linked = await db.transactions.get(row.transaction.id);
				if (linked) {
					await db.transactions.put({
						...linked,
						amountCents: row.transaction.amountCents,
						categoryId: row.transaction.categoryId,
						paymentMethod: row.transaction.paymentMethod,
						note: row.title,
						occurredAt: row.occurredAt ?? linked.occurredAt,
						updatedAt: ts,
						_pending: 1
					});
				}
			});
			await enqueue(db, 'ticket', 'upsert', id, toTicketInput(row, attachmentIds), ts);
			return row;
		},

		async deleteTicket(id: string): Promise<null> {
			const ts = nowIso();
			const cur = await db.tickets.get(id);
			// 票与联动交易同时立墓碑（契约 §5：软删票 + 关联交易）
			await db.transaction('rw', db.tickets, db.transactions, async () => {
				await db.tickets.update(id, { deletedAt: ts, updatedAt: ts, _pending: 1 } as never);
				if (cur) {
					await db.transactions.update(cur.transaction.id, {
						deletedAt: ts,
						updatedAt: ts,
						_pending: 1
					} as never);
				}
			});
			await enqueue(db, 'ticket', 'delete', id, { id }, ts);
			return null;
		},

		async upload(file: File | Blob): Promise<Attachment> {
			// 在线：直传，拿到真 id（票根提交时 attachmentIds 里就是真 id）
			if (isOnline()) {
				try {
					return await api.upload(file);
				} catch {
					// 上传失败（网络抖动）→ 退回离线路径，别让用户白拍
				}
			}
			// 离线：blob 落本地，返回负数临时 id；flush 时换真 id
			const blobId = await db.blobs.add({ blob: file, createdAt: nowIso() });
			const url = URL.createObjectURL(file);
			return { id: -blobId, url, thumbUrl: url, w: 0, h: 0 };
		}
	};
}
