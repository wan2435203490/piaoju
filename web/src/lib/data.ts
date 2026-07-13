/**
 * 统一读入口（W5）—— 页面读数据一律走这里，不再直接调 api.list*。
 *
 * 分派规则：本地库已建立（登录后 initOffline 成功）→ 读本地 Dexie；否则 → 读 API。
 * 本地库是已同步数据的完整镜像，后台 sync 引擎持续把服务端增量合并进来，所以
 * 「优先本地」既快又能离线工作，且不会读到陈旧数据。
 *
 * 读到的实体带可选 `_pending` 字段（1 = 有未推送的本地改动），UI 据此打「待同步」标记；
 * 在线直读 API 时该字段为 undefined，标记自然不显示。
 *
 * db 层走动态 import：Dexie 不进首屏包（conventions §5）。
 *
 *   ✅ 页面读数据：import { data } from '$lib/data'
 *   ✅ 页面写数据：import { outbox } from '$lib/db/outbox'
 *   ❌ 页面直接 api.listTransactions / 裸 fetch
 */
import { api } from '$lib/api/client';
import type {
	Category,
	ListPage,
	MonthlyStats,
	Ticket,
	TicketQuery,
	TicketStats,
	Transaction,
	TransactionQuery
} from '$lib/api/types';
import { isOfflineReady } from '$lib/db/outbox';

/** 本地实体：多一个待同步标记 */
type Pending<T> = T & { _pending?: 0 | 1 };

/** 拿到已打开的本地库；未建立离线库时返回 null（调用方回退 API） */
async function localDB() {
	if (!isOfflineReady()) return null;
	const [{ currentDB }, read] = await Promise.all([import('$lib/db/schema'), import('$lib/db/read')]);
	const db = currentDB();
	return db ? { db, read } : null;
}

export const data = {
	async listTransactions(q: TransactionQuery = {}): Promise<ListPage<Pending<Transaction>>> {
		const local = await localDB();
		return local ? local.read.listTransactions(local.db, q) : api.listTransactions(q);
	},

	async listTickets(q: TicketQuery = {}): Promise<ListPage<Pending<Ticket>>> {
		const local = await localDB();
		return local ? local.read.listTickets(local.db, q) : api.listTickets(q);
	},

	async getTicket(id: string): Promise<Pending<Ticket>> {
		const local = await localDB();
		if (!local) return api.getTicket(id);
		const row = await local.read.getTicket(local.db, id);
		// 本地没有（比如深链进入一张尚未 pull 到的票）→ 回落 API
		return row ?? (await api.getTicket(id));
	},

	async listCategories(): Promise<Category[]> {
		const local = await localDB();
		if (!local) return api.listCategories();
		const rows = await local.read.listCategories(local.db);
		// 本地分类为空说明首轮 pull 还没落地 → 回落 API，别让快记面板没分类可选
		return rows.length > 0 ? rows : api.listCategories();
	},

	async statsMonthly(month: string): Promise<MonthlyStats> {
		const local = await localDB();
		return local ? local.read.statsMonthly(local.db, month) : api.statsMonthly(month);
	},

	async statsTickets(year: number): Promise<TicketStats> {
		const local = await localDB();
		return local ? local.read.statsTickets(local.db, year) : api.statsTickets(year);
	},

	/** 待同步条数（0 = 全部已推送）。无离线库时恒为 0。 */
	async pendingCount(): Promise<number> {
		const local = await localDB();
		return local ? local.read.pendingCount(local.db) : 0;
	}
};
