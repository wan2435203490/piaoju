/**
 * 删除票根的 5 秒可撤销窗口（design skill §4：删除后 5 秒 toast 可撤销）。
 *
 * 详情页确认删除 → schedule(ticket) 并返回票夹列表；
 * 列表页据 current 隐藏该条并渲染撤销 toast；
 * 5 秒内 undo() 取消，超时后真正走 outbox.deleteTicket（软删）。
 */
import type { Ticket } from '$lib/api/types';
import { outbox } from '$lib/db/outbox';

const UNDO_WINDOW_MS = 5000;

interface Pending {
	ticket: Ticket;
	timer: ReturnType<typeof setTimeout>;
}

let pending = $state<Pending | null>(null);
let lastCommittedId = $state<string | null>(null);

async function commit(ticket: Ticket): Promise<void> {
	lastCommittedId = ticket.id;
	try {
		await outbox.deleteTicket(ticket.id);
	} catch {
		// 静默失败（design §4「失败不弹错」）；真实后端阶段由 W5 离线队列重试兜底
	}
}

export const pendingDelete = {
	/** 等待删除中的票（列表页据此隐藏条目 + 渲染撤销 toast） */
	get current(): Ticket | null {
		return pending?.ticket ?? null;
	},

	/** 最近一次已真正提交删除的 id（列表页过滤本地残留条目） */
	get committedId(): string | null {
		return lastCommittedId;
	},

	/** 调度删除；已有等待中的删除会先立即提交 */
	schedule(ticket: Ticket): void {
		if (pending) {
			clearTimeout(pending.timer);
			void commit(pending.ticket);
		}
		const timer = setTimeout(() => {
			pending = null;
			void commit(ticket);
		}, UNDO_WINDOW_MS);
		pending = { ticket, timer };
	},

	/** 撤销等待中的删除（软删本来就支持，这里连请求都还没发） */
	undo(): void {
		if (!pending) return;
		clearTimeout(pending.timer);
		pending = null;
	}
};
