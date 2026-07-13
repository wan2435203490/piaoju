/**
 * 同步引擎（W5）—— outbox 出队推送 + pull 合并的调度层。契约 §8。
 *
 * push 流程
 *   1. 取 outbox 中到期（nextAttemptAt <= now）的项，按 seq FIFO
 *   2. ticket upsert 的 attachmentIds 含负数临时 id（离线拍的照片）→ 先把
 *      blobs 里的 blob 传上去换真 id，再替换 payload
 *   3. 批量 POST /sync/push，按 results 逐条处置：
 *        applied → 出队、清 _pending
 *        stale   → 出队（服务端版本更新，等 pull 覆盖本地）
 *        error   → 留队 + 指数退避；4xx 参数类错误（非网络）留队但标 lastError，
 *                  由 UI 暴露给用户，不静默丢数据
 *   4. 网络整体失败（离线/5xx）→ 全批留队退避，等下次触发
 *
 * 触发时机：应用启动、online 事件、写操作入队后、定时心跳。
 * 单飞：同一时刻只有一个 flush 在跑（flushing 标志）。
 */
import { api } from '$lib/api/client';
import { ApiError } from '$lib/api/types';
import type { SyncChange, SyncResult } from '$lib/api/types';
import { emitSync } from './bus';
import {
	currentDB,
	getCursor,
	setCursor,
	type OutboxItem,
	type PiaojuDB
} from './schema';
import { mergePull } from './merge';

/** 退避：1s、2s、4s… 上限 5min */
const BACKOFF_BASE_MS = 1000;
const BACKOFF_MAX_MS = 5 * 60 * 1000;
/** 单批推送条数上限（服务端 push 无硬上限，控体积） */
const PUSH_BATCH = 50;
/** 心跳：在线时定期 flush + pull */
const HEARTBEAT_MS = 60_000;

function backoffMs(attempts: number): number {
	return Math.min(BACKOFF_BASE_MS * 2 ** attempts, BACKOFF_MAX_MS);
}

export function isOnline(): boolean {
	return typeof navigator === 'undefined' ? true : navigator.onLine;
}

/* ============ 离线照片：临时负 id → 真 attachment id ============ */

/**
 * 把 payload.attachmentIds 里的负数临时 id 换成服务端真 id。
 * 负 id 的 blob 存在 blobs 表（键 = -tempId）。上传成功后删本地 blob。
 * 单张失败 → 抛错，整条 ticket 留队重试（照片和票根必须一起落地）。
 */
async function resolveAttachments(db: PiaojuDB, payload: Record<string, unknown>): Promise<void> {
	const ids = payload.attachmentIds;
	if (!Array.isArray(ids) || !ids.some((n) => typeof n === 'number' && n < 0)) return;

	const resolved: number[] = [];
	for (const id of ids as number[]) {
		if (id >= 0) {
			resolved.push(id);
			continue;
		}
		const row = await db.blobs.get(-id);
		if (!row) continue; // blob 已丢（清缓存等）：跳过该图，不阻塞票根落地
		const attachment = await api.upload(row.blob);
		resolved.push(attachment.id);
		await db.blobs.delete(-id);
	}
	payload.attachmentIds = resolved;
}

/* ============ push ============ */

async function applyResult(db: PiaojuDB, item: OutboxItem, result: SyncResult): Promise<void> {
	if (result.status === 'applied' || result.status === 'stale') {
		// stale：服务端版本更新，本地改动作废——出队，pull 会带回权威版本
		await db.outbox.delete(item.seq!);
		const table = item.entity === 'transaction' ? db.transactions : db.tickets;
		// 只有当队列里没有该 id 的其它待推项时才清 _pending（连续编辑会有多条）
		const remaining = await db.outbox.where('id').equals(item.id).count();
		if (remaining === 0) {
			await table.update(item.id, { _pending: 0 } as never);
		}
		return;
	}
	// error：留队退避，记 code 供 UI 展示
	await db.outbox.update(item.seq!, {
		attempts: item.attempts + 1,
		nextAttemptAt: Date.now() + backoffMs(item.attempts),
		lastError: `code ${result.code}`
	});
}

async function retryLater(db: PiaojuDB, items: OutboxItem[], reason: string): Promise<void> {
	const now = Date.now();
	await db.transaction('rw', db.outbox, async () => {
		for (const item of items) {
			await db.outbox.update(item.seq!, {
				attempts: item.attempts + 1,
				nextAttemptAt: now + backoffMs(item.attempts),
				lastError: reason
			});
		}
	});
}

let flushing = false;

/**
 * 推一轮 outbox。返回本轮成功落地的条数。
 * 离线、无到期项、已有 flush 在跑 → 直接返回 0。
 */
export async function flush(): Promise<number> {
	const db = currentDB();
	if (!db || flushing || !isOnline()) return 0;

	flushing = true;
	try {
		const due = await db.outbox
			.where('nextAttemptAt')
			.belowOrEqual(Date.now())
			.sortBy('seq');
		if (due.length === 0) return 0;

		const batch = due.slice(0, PUSH_BATCH);
		const changes: SyncChange[] = [];
		const sendable: OutboxItem[] = [];

		for (const item of batch) {
			try {
				if (item.entity === 'ticket' && item.op === 'upsert') {
					await resolveAttachments(db, item.payload);
				}
			} catch (err) {
				// 照片上传失败：该条留队重试，不拖累同批其它项
				await retryLater(db, [item], err instanceof ApiError ? `upload ${err.code}` : 'upload failed');
				continue;
			}
			changes.push({
				entity: item.entity,
				op: item.op,
				payload: item.payload as SyncChange['payload'],
				clientUpdatedAt: item.clientUpdatedAt
			});
			sendable.push(item);
		}
		if (sendable.length === 0) return 0;

		let results: SyncResult[];
		try {
			({ results } = await api.syncPush(changes));
		} catch (err) {
			// 网络/服务端整体失败 → 全批退避
			await retryLater(db, sendable, err instanceof ApiError ? `push ${err.code}` : 'offline');
			return 0;
		}

		let applied = 0;
		for (let i = 0; i < sendable.length; i++) {
			const item = sendable[i]!;
			const result = results[i] ?? { id: item.id, status: 'error' as const, code: -1 };
			await applyResult(db, item, result);
			if (result.status === 'applied') applied++;
		}
		return applied;
	} finally {
		flushing = false;
	}
}

/* ============ pull ============ */

let pulling = false;

/** 增量拉取并合并进本地库。返回合并的行数。 */
export async function pull(): Promise<number> {
	const db = currentDB();
	if (!db || pulling || !isOnline()) return 0;

	pulling = true;
	try {
		let merged = 0;
		let cursor = await getCursor(db);
		// hasMore 循环：一次拉完积压（首次登录可能多页）
		for (;;) {
			const page = await api.syncPull(cursor, 200);
			merged += await mergePull(db, page);
			cursor = page.nextCursor;
			await setCursor(db, cursor);
			if (!page.hasMore) break;
		}
		return merged;
	} catch {
		return 0; // 离线/失败：静默，下次心跳重试
	} finally {
		pulling = false;
	}
}

/** 一轮完整同步：先推后拉（推完再拉，避免刚推的改动被旧快照覆盖） */
export async function syncOnce(): Promise<void> {
	const pushed = await flush();
	const pulled = await pull();
	// 有实际变更才通知页面重读（空转的心跳不触发无谓重渲染）
	if (pushed > 0 || pulled > 0) emitSync();
}

/* ============ 调度 ============ */

let started = false;
let heartbeat: ReturnType<typeof setInterval> | null = null;

/** 启动同步调度（登录后调用）：立即同步一轮 + 监听 online + 心跳 */
export function startSync(): void {
	if (started || typeof window === 'undefined') return;
	started = true;

	void syncOnce();
	window.addEventListener('online', onOnline);
	heartbeat = setInterval(() => {
		if (isOnline()) void syncOnce();
	}, HEARTBEAT_MS);
}

/** 停止调度（登出） */
export function stopSync(): void {
	if (!started || typeof window === 'undefined') return;
	started = false;
	window.removeEventListener('online', onOnline);
	if (heartbeat) clearInterval(heartbeat);
	heartbeat = null;
}

function onOnline(): void {
	void syncOnce();
}

/** 写操作入队后调用：在线则立刻推（不等心跳），离线是 no-op */
export function kick(): void {
	if (isOnline()) void flush();
}
