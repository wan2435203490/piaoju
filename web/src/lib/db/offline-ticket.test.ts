/**
 * Wave 4 测试卡 2：离线建票带照片（docs/PLAN.md）。
 *
 * 这条路径是离线引擎里最容易出错的一段，三个必须成立的性质：
 * 1. 离线建票 → 联动交易立刻进本地账本（靠客户端生成的 transactionId，契约 §5 v1.2）
 * 2. 离线拍的照片 → blob 落本地、attachmentIds 里是负数临时 id
 * 3. 联网 flush → 先把 blob 传上去换真 attachment id，再带着真 id push 票根
 */
import 'fake-indexeddb/auto';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import type { TicketInput } from '$lib/api/types';

// api 必须在 import 被测模块之前 mock（它们在模块顶层就 import 了 api）
const upload = vi.fn();
const syncPush = vi.fn();
const syncPull = vi.fn();
vi.mock('$lib/api/client', () => ({
	api: {
		upload: (...a: unknown[]) => upload(...a),
		syncPush: (...a: unknown[]) => syncPush(...a),
		syncPull: (...a: unknown[]) => syncPull(...a)
	}
}));

import { createOfflineOutbox } from './offline-outbox';
import { closeDB, openDB, type PiaojuDB } from './schema';
import { flush } from './sync-engine';

const TICKET_ID = 'b2222222-2222-4222-8222-222222222222';
const TX_ID = 'c3333333-3333-4333-8333-333333333333';

let db: PiaojuDB;
let dbSeq = 100;
let online = false;

function ticketInput(over: Partial<TicketInput> = {}): TicketInput {
	return {
		id: TICKET_ID,
		transactionId: TX_ID,
		kind: 'movie',
		title: '沙丘 3',
		venue: '万达影城',
		eventTime: '2026-07-12T19:30:00.000Z',
		seat: '7排8座',
		extra: { cinema: '万达影城', hall: 'IMAX厅', filmFormat: 'IMAX' },
		rating: 5,
		memo: '',
		amountCents: 4500,
		categoryId: 5,
		paymentMethod: 'alipay',
		occurredAt: '2026-07-12T19:00:00.000Z',
		attachmentIds: [],
		...over
	} as TicketInput;
}

beforeEach(() => {
	upload.mockReset();
	syncPush.mockReset();
	syncPull.mockReset();
	online = false;
	// navigator.onLine 由用例控制离线/在线
	vi.stubGlobal('navigator', { onLine: false });
	Object.defineProperty(globalThis.navigator, 'onLine', { get: () => online, configurable: true });
	// jsdom 之外没有 URL.createObjectURL
	vi.stubGlobal('URL', { ...URL, createObjectURL: () => 'blob:local-preview' });
	db = openDB(++dbSeq);
});

afterEach(() => {
	closeDB();
	vi.unstubAllGlobals();
});

describe('离线建票带照片', () => {
	it('离线拍照 → blob 存本地，返回负数临时 attachment id', async () => {
		const outbox = createOfflineOutbox(dbSeq);

		const att = await outbox.upload(new Blob(['fake-jpeg']));

		expect(upload).not.toHaveBeenCalled(); // 离线不该发网络请求
		expect(att.id).toBeLessThan(0); // 负数临时 id
		expect(await db.blobs.count()).toBe(1); // blob 落了本地
	});

	it('离线建票 → 联动交易立刻进本地账本（离线也能看到这笔支出）', async () => {
		const outbox = createOfflineOutbox(dbSeq);

		await outbox.createTicket(ticketInput());

		const linked = await db.transactions.get(TX_ID);
		expect(linked).toBeDefined();
		expect(linked?.amountCents).toBe(4500);
		expect(linked?.direction).toBe('expense');
		expect(linked?.note).toBe('沙丘 3'); // 契约 §5：note = title 快照
		expect(linked?.ticketId).toBe(TICKET_ID);
		expect(linked?._pending).toBe(1); // 待同步

		// 票本身也在，且带待同步标记
		expect((await db.tickets.get(TICKET_ID))?._pending).toBe(1);
		// 队列里只有票这一条（联动交易随票一起 push，不单独入队）
		expect(await db.outbox.count()).toBe(1);
	});

	it('联网 flush → blob 先换真 attachment id，再带真 id push 票根', async () => {
		const outbox = createOfflineOutbox(dbSeq);

		// 离线：拍照 + 建票
		const att = await outbox.upload(new Blob(['fake-jpeg']));
		await outbox.createTicket(ticketInput({ attachmentIds: [att.id] }));
		expect(att.id).toBeLessThan(0);

		// 联网：上传返回真 id 77，push 全部 applied
		online = true;
		upload.mockResolvedValue({ id: 77, url: '/uploads/x.jpg', thumbUrl: '/uploads/t.jpg', w: 100, h: 100 });
		syncPush.mockResolvedValue({ results: [{ id: TICKET_ID, status: 'applied', code: 0 }] });

		const applied = await flush();

		expect(applied).toBe(1);
		expect(upload).toHaveBeenCalledTimes(1); // blob 被传了

		// push 出去的 payload 里必须是真 id，不能是负数
		const pushed = syncPush.mock.calls[0]![0] as { payload: { attachmentIds: number[] } }[];
		expect(pushed[0]!.payload.attachmentIds).toEqual([77]);

		// 收尾：blob 已清、队列已空、待同步标记已清
		expect(await db.blobs.count()).toBe(0);
		expect(await db.outbox.count()).toBe(0);
		expect((await db.tickets.get(TICKET_ID))?._pending).toBe(0);
	});

	it('push 网络失败 → 留队重试，数据不丢、待同步标记不清', async () => {
		const outbox = createOfflineOutbox(dbSeq);
		await outbox.createTicket(ticketInput());

		online = true;
		syncPush.mockRejectedValue(new Error('network down'));

		expect(await flush()).toBe(0);

		const queued = await db.outbox.toArray();
		expect(queued).toHaveLength(1); // 还在队里
		expect(queued[0]!.attempts).toBe(1); // 记了一次失败
		expect(queued[0]!.nextAttemptAt).toBeGreaterThan(Date.now()); // 退避中
		expect((await db.tickets.get(TICKET_ID))?._pending).toBe(1); // 圆点还在
	});

	it('服务端判 stale → 出队并清标记（本地改动作废，等 pull 带回权威版本）', async () => {
		const outbox = createOfflineOutbox(dbSeq);
		await outbox.createTicket(ticketInput());

		online = true;
		syncPush.mockResolvedValue({ results: [{ id: TICKET_ID, status: 'stale', code: 40902 }] });

		await flush();

		expect(await db.outbox.count()).toBe(0); // 不再重试（重试也还是 stale）
		expect((await db.tickets.get(TICKET_ID))?._pending).toBe(0);
	});

	it('连续编辑同一张票 → 未发送的队列项被合并，只推最后一次', async () => {
		const outbox = createOfflineOutbox(dbSeq);

		await outbox.createTicket(ticketInput({ title: '第一版' }));
		await outbox.updateTicket(TICKET_ID, { title: '第二版' });
		await outbox.updateTicket(TICKET_ID, { title: '第三版' });

		const queued = await db.outbox.toArray();
		expect(queued).toHaveLength(1); // 三次编辑合并成一条
		expect((queued[0]!.payload as { title: string }).title).toBe('第三版');
	});
});
