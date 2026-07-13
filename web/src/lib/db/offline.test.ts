/**
 * Wave 4 测试卡：离线引擎的三个风险点（docs/PLAN.md）。
 *
 * 1. 双端并发改同条记录 → LWW 胜负正确、本地未推送改动不被服务端旧快照顶掉
 * 2. 离线建票带照片 → 联动交易立刻进本地账本；blob 入队，flush 时换真 attachment id
 * 3. 时钟漂移 → 本地时钟慢时改动不会被永久判 stale
 *
 * IndexedDB 用 fake-indexeddb（devDependency，不进生产包）。
 */
import 'fake-indexeddb/auto';
import { beforeEach, describe, expect, it, vi } from 'vitest';

import { PiaojuDB, type LocalTransaction } from './schema';
import { mergePull } from './merge';
import { clockSkewMs, nowIso, observeServerTime } from './clock';

let db: PiaojuDB;
let dbSeq = 0;

beforeEach(async () => {
	// 每个用例一个全新库（库名递增，避免 fake-indexeddb 跨用例串数据）
	db = new PiaojuDB(++dbSeq);
	await db.open();
});

function tx(over: Partial<LocalTransaction> = {}): LocalTransaction {
	return {
		id: 'a1111111-1111-4111-8111-111111111111',
		amountCents: 1800,
		direction: 'expense',
		categoryId: 2,
		note: '奶茶',
		occurredAt: '2026-07-13T08:30:00.000Z',
		paymentMethod: 'wechat',
		ticketId: null,
		createdAt: '2026-07-13T08:30:00.000Z',
		updatedAt: '2026-07-13T08:30:00.000Z',
		deletedAt: null,
		_pending: 0,
		...over
	};
}

function pullPage(over: Partial<Parameters<typeof mergePull>[1]> = {}) {
	return { transactions: [], tickets: [], categories: [], nextCursor: 'c1', hasMore: false, ...over };
}

describe('测试卡 1：双端并发改同一条记录', () => {
	it('服务端版本更新且本地无待推送改动 → 覆盖本地', async () => {
		await db.transactions.put(tx({ amountCents: 1800, updatedAt: '2026-07-13T08:30:00.000Z' }));

		await mergePull(
			db,
			pullPage({
				transactions: [
					{ ...tx({ amountCents: 2500, updatedAt: '2026-07-13T09:00:00.000Z' }), deletedAt: null }
				]
			})
		);

		const row = await db.transactions.get(tx().id);
		expect(row?.amountCents).toBe(2500); // B 端的改动落地
	});

	it('本地有未推送改动（_pending=1）→ 服务端版本不覆盖，等 push 定胜负', async () => {
		// A 端离线改成 9900，还在 outbox 里排队
		await db.transactions.put(
			tx({ amountCents: 9900, updatedAt: '2026-07-13T10:00:00.000Z', _pending: 1 })
		);

		// 此时 pull 拿到 B 端更早的版本
		await mergePull(
			db,
			pullPage({
				transactions: [
					{ ...tx({ amountCents: 2500, updatedAt: '2026-07-13T09:00:00.000Z' }), deletedAt: null }
				]
			})
		);

		const row = await db.transactions.get(tx().id);
		// 本地改动必须留住 —— 覆盖会让用户眼睁睁看着自己刚写的东西被顶掉
		expect(row?.amountCents).toBe(9900);
		expect(row?._pending).toBe(1);
	});

	it('服务端墓碑落地 → 本地保留行但标 deletedAt（不能直接删行，否则被旧快照复活）', async () => {
		await db.transactions.put(tx());

		await mergePull(
			db,
			pullPage({
				transactions: [
					{
						...tx({ updatedAt: '2026-07-13T11:00:00.000Z' }),
						deletedAt: '2026-07-13T11:00:00.000Z'
					}
				]
			})
		);

		const row = await db.transactions.get(tx().id);
		expect(row).toBeDefined(); // 行还在
		expect(row?.deletedAt).toBe('2026-07-13T11:00:00.000Z'); // 但已是墓碑
	});

	it('服务端版本更旧 → 不回退本地（LWW：新的赢）', async () => {
		await db.transactions.put(tx({ amountCents: 2500, updatedAt: '2026-07-13T09:00:00.000Z' }));

		await mergePull(
			db,
			pullPage({
				transactions: [
					{ ...tx({ amountCents: 1800, updatedAt: '2026-07-13T08:00:00.000Z' }), deletedAt: null }
				]
			})
		);

		expect((await db.transactions.get(tx().id))?.amountCents).toBe(2500);
	});
});

describe('测试卡 3：时钟漂移', () => {
	it('本地时钟慢 → 用服务端下发的 updatedAt 校正，clientUpdatedAt 不再落后', () => {
		// 本地时钟停在 10:00，服务端已经是 10:05（本地慢了 5 分钟）
		vi.setSystemTime(new Date('2026-07-13T10:00:00.000Z'));
		const serverSaw = '2026-07-13T10:05:00.000Z';

		// 校正前：本地时间戳比服务端版本旧 → push 必被判 stale → 改动永远推不上去
		expect(nowIso() < serverSaw).toBe(true);

		observeServerTime(serverSaw);

		// 校正后：本地时间戳追上服务端时钟，改动能被接受
		expect(clockSkewMs()).toBeGreaterThanOrEqual(5 * 60 * 1000);
		expect(nowIso() >= serverSaw).toBe(true);

		vi.useRealTimers();
	});

	it('本地时钟快 → 不回拨（本地快只是自己在 LWW 里占优，回拨反而制造新的 stale）', () => {
		vi.setSystemTime(new Date('2026-07-13T12:00:00.000Z'));
		const before = clockSkewMs();

		observeServerTime('2026-07-13T10:00:00.000Z'); // 服务端时间更早

		expect(clockSkewMs()).toBe(before); // 偏移不变，不减
		vi.useRealTimers();
	});
});
