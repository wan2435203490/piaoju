/**
 * pull 合并（W5）—— 把服务端增量写进本地库。契约 §8。
 *
 * 冲突规则
 * - 本地行 _pending=1（有未推送改动）→ 服务端版本不覆盖。本地改动还在 outbox 里
 *   排队，胜负由服务端 push 的 LWW 裁定；此刻覆盖会让用户眼睁睁看着自己刚写的
 *   东西被旧数据顶掉。push 返回 stale 时会清掉本地改动，下一轮 pull 自然收敛。
 * - 否则按 updatedAt LWW：服务端 updatedAt >= 本地 → 覆盖（含墓碑）。
 *   时钟漂移：updatedAt 一律取服务端值（客户端从不自己写这个字段），
 *   故比较的是同一时钟下的两个值，本地系统时间跑偏不影响判定。
 * - 墓碑（deletedAt 非 null）：保留行、写入 deletedAt，读查询过滤。直接删行会被
 *   后续 pull 的旧快照复活。
 */
import type { SyncPullData, Tombstone } from '$lib/api/types';
import { observeServerTime } from './clock';
import type { Local, PiaojuDB } from './schema';

/** 服务端行是否该覆盖本地行 */
function shouldOverwrite<T extends { updatedAt: string }>(
	local: Local<T> | undefined,
	remote: Tombstone<T>
): boolean {
	if (!local) return true;
	if (local._pending === 1) return false; // 本地未推送改动优先
	return remote.updatedAt >= local.updatedAt; // RFC3339 UTC 定长，字典序 = 时序
}

/** 合并一页 pull 结果，返回写入的行数 */
export async function mergePull(db: PiaojuDB, page: SyncPullData): Promise<number> {
	let written = 0;

	// 服务端下发的 updatedAt 是服务端时钟的样本 —— 用来校正本地时钟偏移。
	// 本地时钟慢会让 clientUpdatedAt 永远小于服务端版本 → push 永远判 stale → 改动
	// 永远推不上去（见 clock.ts）。
	for (const row of [...page.transactions, ...page.tickets]) {
		observeServerTime(row.updatedAt);
	}

	await db.transaction('rw', db.transactions, db.tickets, db.categories, async () => {
		for (const remote of page.transactions) {
			const local = await db.transactions.get(remote.id);
			if (!shouldOverwrite(local, remote)) continue;
			await db.transactions.put({ ...remote, _pending: 0 });
			written++;
		}

		for (const remote of page.tickets) {
			const local = await db.tickets.get(remote.id);
			if (!shouldOverwrite(local, remote)) continue;
			await db.tickets.put({ ...remote, _pending: 0 });
			written++;
		}

		// categories 无本地写路径（只在「我的」页增删，走在线直写），直接覆盖
		for (const remote of page.categories) {
			await db.categories.put({ ...remote, _pending: 0 });
			written++;
		}
	});

	return written;
}
