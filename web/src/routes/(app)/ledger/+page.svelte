<script lang="ts">
	import { onMount } from 'svelte';
	import { fly } from 'svelte/transition';
	import { api } from '$lib/api/client';
	import { outbox } from '$lib/db/outbox';
	import type { Category, Transaction, TransactionInput } from '$lib/api/types';
	import Amount from '$lib/components/Amount.svelte';
	import Button from '$lib/components/Button.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Sheet from '$lib/components/Sheet.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import MonthSwitcher from '$lib/components/ledger/MonthSwitcher.svelte';
	import MonthSummaryCard from '$lib/components/ledger/MonthSummaryCard.svelte';
	import QuickAddSheet from '$lib/components/ledger/QuickAddSheet.svelte';
	import TransactionRow from '$lib/components/ledger/TransactionRow.svelte';
	import UndoToast from '$lib/components/ledger/UndoToast.svelte';
	import {
		PAYMENT_LABELS,
		currentMonth,
		dayHeading,
		localDayKey,
		motionDur,
		timeHM
	} from '$lib/components/ledger/format';

	/* ================= 状态 ================= */

	let month = $state(currentMonth());
	let items = $state<Transaction[]>([]);
	let nextCursor = $state<string | null>(null);
	let categories = $state<Category[]>([]);
	let summary = $state({ expenseCents: 0, incomeCents: 0 });

	let initialLoading = $state(true);
	let refetching = $state(false); // 换月：旧内容降透明度保留，不闪骨架
	let loadingMore = $state(false);
	let loadError = $state('');

	/** 离线队列尚未确认的 id（「待同步」小圆点） */
	let pendingIds = $state<string[]>([]);

	let quickAddOpen = $state(false);

	/* 详情 / 删除二次确认 */
	let detailTx = $state<Transaction | null>(null);
	let detailOpen = $state(false);
	let confirmDelete = $state(false);

	/* 删除后 5 秒可撤销 */
	let toastOpen = $state(false);
	let undoTx = $state<Transaction | null>(null);

	let requestSeq = 0; // 丢弃换月竞态下的过期响应

	/* ================= 数据加载 ================= */

	async function loadMonth(target: string, initial = false) {
		const seq = ++requestSeq;
		if (initial) initialLoading = true;
		else refetching = true;
		loadError = '';
		try {
			const [page, stats] = await Promise.all([
				api.listTransactions({ month: target, limit: 50 }),
				api.statsMonthly(target)
			]);
			if (seq !== requestSeq) return;
			items = page.items;
			nextCursor = page.nextCursor;
			summary = { expenseCents: stats.expenseCents, incomeCents: stats.incomeCents };
		} catch (error) {
			if (seq !== requestSeq) return;
			loadError = error instanceof Error ? error.message : '加载失败';
		} finally {
			if (seq === requestSeq) {
				initialLoading = false;
				refetching = false;
			}
		}
	}

	async function loadMore() {
		if (!nextCursor || loadingMore || refetching) return;
		loadingMore = true;
		try {
			const page = await api.listTransactions({ month, cursor: nextCursor, limit: 50 });
			items = [...items, ...page.items];
			nextCursor = page.nextCursor;
		} catch {
			// 滚动加载失败静默，下次进入视口重试
		} finally {
			loadingMore = false;
		}
	}

	function switchMonth(next: string) {
		month = next;
		void loadMonth(next);
	}

	onMount(() => {
		void loadMonth(month, true);
		api
			.listCategories()
			.then((list) => (categories = list))
			.catch(() => {
				/* 分类加载失败时行内回退到「未分类」展示 */
			});
	});

	/* ================= 派生 ================= */

	const categoryMap = $derived(new Map(categories.map((c) => [c.id, c])));

	interface DayGroup {
		key: string;
		heading: string;
		expense: number;
		income: number;
		txs: Transaction[];
	}

	/** items 已按 occurredAt desc，同日必然连续 → 单趟分组 */
	const groups = $derived.by((): DayGroup[] => {
		const out: DayGroup[] = [];
		for (const tx of items) {
			const key = localDayKey(tx.occurredAt);
			let group = out[out.length - 1];
			if (!group || group.key !== key) {
				group = { key, heading: dayHeading(key), expense: 0, income: 0, txs: [] };
				out.push(group);
			}
			group.txs.push(tx);
			if (tx.direction === 'expense') group.expense += tx.amountCents;
			else group.income += tx.amountCents;
		}
		return out;
	});

	/* ================= 快记（写一律走 outbox） ================= */

	function bumpSummary(tx: { direction: string; amountCents: number }, sign: 1 | -1) {
		if (tx.direction === 'expense') {
			summary = { ...summary, expenseCents: summary.expenseCents + sign * tx.amountCents };
		} else {
			summary = { ...summary, incomeCents: summary.incomeCents + sign * tx.amountCents };
		}
	}

	function handleQuickAdd(input: TransactionInput) {
		const now = new Date().toISOString();
		const optimistic: Transaction = { ...input, ticketId: null, createdAt: now, updatedAt: now };
		// 快记时间恒为「现在」；仅当正看本月时插入列表顶部（fly-in）
		if (input.occurredAt.slice(0, 7) === month) {
			items = [optimistic, ...items];
			bumpSummary(optimistic, 1);
		}
		pendingIds = [...pendingIds, input.id];
		outbox
			.createTransaction(input)
			.then((server) => {
				items = items.map((t) => (t.id === server.id ? server : t));
				pendingIds = pendingIds.filter((id) => id !== server.id);
			})
			.catch(() => {
				// design §4：失败不弹错 —— 条目保留「待同步」小圆点
			});
	}

	/* ================= 详情 / 删除 / 撤销 ================= */

	function openDetail(tx: Transaction) {
		detailTx = tx;
		confirmDelete = false;
		detailOpen = true;
	}

	function insertSorted(tx: Transaction) {
		const index = items.findIndex((t) => t.occurredAt <= tx.occurredAt);
		items = index === -1 ? [...items, tx] : [...items.slice(0, index), tx, ...items.slice(index)];
	}

	function doDelete() {
		const tx = detailTx;
		if (!tx) return;
		detailOpen = false;
		confirmDelete = false;
		items = items.filter((t) => t.id !== tx.id);
		bumpSummary(tx, -1);
		undoTx = tx;
		toastOpen = true;
		outbox.deleteTransaction(tx.id).catch(() => {
			// 删除请求失败：恢复条目
			toastOpen = false;
			undoTx = null;
			insertSorted(tx);
			bumpSummary(tx, 1);
		});
	}

	function undoDelete() {
		const tx = undoTx;
		if (!tx) return;
		toastOpen = false;
		undoTx = null;
		insertSorted(tx);
		bumpSummary(tx, 1);
		// 同 id 幂等 upsert（conventions §1）即可复活软删条目
		const input: TransactionInput = {
			id: tx.id,
			amountCents: tx.amountCents,
			direction: tx.direction,
			categoryId: tx.categoryId,
			note: tx.note,
			occurredAt: tx.occurredAt,
			paymentMethod: tx.paymentMethod
		};
		pendingIds = [...pendingIds, tx.id];
		outbox
			.createTransaction(input)
			.then((server) => {
				items = items.map((t) => (t.id === server.id ? server : t));
				pendingIds = pendingIds.filter((id) => id !== server.id);
			})
			.catch(() => {
				/* 保留待同步圆点 */
			});
	}

	/* ================= 无限滚动 ================= */

	let sentinel = $state<HTMLElement | null>(null);

	$effect(() => {
		const el = sentinel;
		if (!el) return;
		const observer = new IntersectionObserver(
			(entries) => {
				if (entries.some((entry) => entry.isIntersecting)) void loadMore();
			},
			{ rootMargin: '240px' }
		);
		observer.observe(el);
		return () => observer.disconnect();
	});
</script>

<svelte:head>
	<title>账本 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<div>
		<h1>账本</h1>
		<p class="sub">月度流水 · 快记</p>
	</div>
	<div class="head-tools">
		<a class="stats-link" href="/stats" aria-label="查看统计">📊 统计</a>
	</div>
</header>

<div class="month-row">
	<MonthSwitcher value={month} onchange={switchMonth} />
</div>

{#if initialLoading}
	<div class="skeletons" aria-label="加载中">
		<Skeleton lines={1} height="88px" />
		<Skeleton lines={6} height="52px" />
	</div>
{:else if loadError}
	<EmptyState
		emoji="⚠️"
		title="加载失败"
		description={loadError}
		actionLabel="重试"
		onaction={() => void loadMonth(month, true)}
	/>
{:else}
	<div class="content" class:refetching>
		<MonthSummaryCard expenseCents={summary.expenseCents} incomeCents={summary.incomeCents} />

		{#if groups.length === 0}
			<EmptyState
				emoji="🧾"
				title="还没有一笔账"
				description="第一笔账只要 3 秒，从右下角开始"
				actionLabel="记一笔"
				onaction={() => (quickAddOpen = true)}
			/>
		{:else}
			{#each groups as group (group.key)}
				<section class="day">
					<header class="day-head">
						<h2>{group.heading}</h2>
						<div class="day-total">
							{#if group.expense > 0}
								<span>支 <Amount cents={group.expense} direction="expense" /></span>
							{/if}
							{#if group.income > 0}
								<span>收 <Amount cents={group.income} direction="income" /></span>
							{/if}
						</div>
					</header>
					<div class="day-card">
						{#each group.txs as tx (tx.id)}
							<div class="row-wrap" transition:fly={{ y: -8, duration: motionDur(150) }}>
								<TransactionRow
									{tx}
									category={categoryMap.get(tx.categoryId)}
									pending={pendingIds.includes(tx.id)}
									onclick={() => openDetail(tx)}
								/>
							</div>
						{/each}
					</div>
				</section>
			{/each}

			{#if nextCursor}
				<div class="sentinel" bind:this={sentinel}>
					{#if loadingMore}
						<Skeleton lines={1} height="52px" />
					{/if}
				</div>
			{:else}
				<p class="feed-end">— 本月到底了 —</p>
			{/if}
		{/if}
	</div>
{/if}

<!-- FAB：56px --brand（design §4） -->
<button type="button" class="fab" aria-label="快记一笔" onclick={() => (quickAddOpen = true)}>
	<svg viewBox="0 0 24 24" aria-hidden="true"><path d="M12 5v14M5 12h14" /></svg>
</button>

<QuickAddSheet bind:open={quickAddOpen} {categories} onsubmit={handleQuickAdd} />

<!-- 详情 + 删除二次确认（ActionSheet 形态，design §4） -->
<Sheet bind:open={detailOpen} title="账目详情" onclose={() => (confirmDelete = false)}>
	{#if detailTx}
		<div class="detail">
			<div class="detail-amount">
				<Amount cents={detailTx.amountCents} direction={detailTx.direction} size="lg" />
			</div>
			<dl class="detail-fields">
				<div>
					<dt>分类</dt>
					<dd>
						{categoryMap.get(detailTx.categoryId)?.icon ?? ''}
						{categoryMap.get(detailTx.categoryId)?.name ?? '未分类'}
					</dd>
				</div>
				{#if detailTx.note}
					<div><dt>备注</dt><dd>{detailTx.note}</dd></div>
				{/if}
				<div>
					<dt>时间</dt>
					<dd class="tnum">{dayHeading(localDayKey(detailTx.occurredAt))} {timeHM(detailTx.occurredAt)}</dd>
				</div>
				<div><dt>支付方式</dt><dd>{PAYMENT_LABELS[detailTx.paymentMethod]}</dd></div>
			</dl>

			{#if detailTx.ticketId}
				<p class="ticket-hint">该笔来自票根，请到票夹中删除对应票据</p>
			{:else if confirmDelete}
				<p class="confirm-hint">确认删除这笔账？删除后 5 秒内可撤销</p>
				<div class="detail-actions">
					<Button variant="danger" block onclick={doDelete}>确认删除</Button>
					<Button variant="ghost" block onclick={() => (confirmDelete = false)}>取消</Button>
				</div>
			{:else}
				<div class="detail-actions">
					<Button variant="danger" block onclick={() => (confirmDelete = true)}>删除</Button>
				</div>
			{/if}
		</div>
	{/if}
</Sheet>

<UndoToast bind:open={toastOpen} message="已删除一笔账" actionLabel="撤销" onaction={undoDelete} />

<style>
	.page-head {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		padding-block: 24px 8px;
	}

	h1 {
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 700;
		color: var(--ink);
		margin: 0;
	}

	.sub {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		margin: 4px 0 0;
	}

	.stats-link {
		display: inline-flex;
		align-items: center;
		min-height: 44px; /* 触控目标 */
		padding: 0 12px;
		border-radius: var(--radius-btn);
		font-size: 0.875rem;
		color: var(--brand);
		text-decoration: none;
		transition: background-color var(--dur-fast) var(--ease);
	}

	.stats-link:active {
		background: var(--bg);
	}

	.month-row {
		display: flex;
		justify-content: center;
		padding-bottom: 8px;
	}

	.skeletons {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.content {
		display: flex;
		flex-direction: column;
		gap: 16px;
		transition: opacity var(--dur-fast) var(--ease);
	}

	/* 换月重取：旧渲染降透明度保留，不闪骨架（dataviz 交互规范同款） */
	.refetching {
		opacity: 0.55;
		pointer-events: none;
	}

	.day-head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		padding: 0 4px 6px;
	}

	.day-head h2 {
		font-size: 0.75rem; /* 12 辅助 */
		font-weight: 600;
		color: var(--ink-2);
		margin: 0;
	}

	.day-total {
		display: flex;
		gap: 12px;
		font-size: 0.75rem;
		color: var(--ink-2);
	}

	.day-card {
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
		overflow: hidden;
	}

	.row-wrap + .row-wrap {
		border-top: 1px solid var(--line);
	}

	.sentinel {
		min-height: 8px;
	}

	.feed-end {
		text-align: center;
		font-size: 0.75rem;
		color: var(--ink-2);
		padding-block: 8px;
	}

	.fab {
		position: fixed;
		right: max(var(--page-inline), env(safe-area-inset-right));
		bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom) + 16px);
		z-index: 20;
		display: grid;
		place-items: center;
		width: 56px;
		height: 56px;
		border: none;
		border-radius: 50%;
		background: var(--brand);
		color: var(--on-brand);
		box-shadow: var(--shadow-card);
		cursor: pointer;
		transition: transform var(--dur-fast) var(--ease);
	}

	.fab:active {
		transform: scale(0.94);
	}

	.fab svg {
		width: 24px;
		height: 24px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2.5;
		stroke-linecap: round;
	}

	/* ---- 详情 sheet ---- */
	.detail {
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding-bottom: 8px;
	}

	.detail-amount {
		text-align: center;
		padding-block: 8px;
	}

	.detail-fields {
		display: flex;
		flex-direction: column;
		gap: 8px;
		margin: 0;
		padding: 12px;
		background: var(--bg);
		border-radius: var(--radius-card);
	}

	.detail-fields > div {
		display: flex;
		justify-content: space-between;
		gap: 16px;
	}

	.detail-fields dt {
		flex: none;
		font-size: 0.875rem;
		color: var(--ink-2);
	}

	.detail-fields dd {
		margin: 0;
		font-size: 0.875rem;
		color: var(--ink);
		text-align: right;
		overflow-wrap: anywhere;
	}

	.ticket-hint,
	.confirm-hint {
		font-size: 0.875rem;
		color: var(--ink-2);
		text-align: center;
		margin: 0;
	}

	.confirm-hint {
		color: var(--danger);
	}

	.detail-actions {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}
</style>
