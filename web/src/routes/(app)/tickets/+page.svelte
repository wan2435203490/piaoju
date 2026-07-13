<script lang="ts">
	/**
	 * 票夹页：卡片墙 / 时间线双视图 + kind/年份筛选 + cursor 分页。
	 * mock 模式（VITE_MOCK=1）数据来自 fixtures，五种票型全部有样本。
	 */
	import { fade } from 'svelte/transition';
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import type { Ticket, TicketKind } from '$lib/api/types';
	import { TICKET_KINDS } from '$lib/api/types';
	import Amount from '$lib/components/Amount.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import TicketCard from '$lib/components/ticket/TicketCard.svelte';
	import { KIND_META, fmtDate, fmtMonth } from '$lib/components/ticket/kinds';
	import { pendingDelete } from '$lib/components/ticket/pending-delete.svelte';

	/* ---------- 筛选 + 视图状态 ---------- */

	type View = 'wall' | 'timeline';

	let view = $state<View>('wall');
	let kind = $state<TicketKind | ''>('');
	let year = $state<number | ''>('');

	const THIS_YEAR = new Date().getFullYear();
	const YEARS = Array.from({ length: 6 }, (_, i) => THIS_YEAR - i);

	/* ---------- 列表加载（cursor 分页） ---------- */

	let items = $state<Ticket[]>([]);
	let nextCursor = $state<string | null>(null);
	let loading = $state(true);
	let loadingMore = $state(false);
	let loadError = $state('');

	/** 竞态防护：筛选连点时只采纳最后一次请求 */
	let requestSeq = 0;

	async function loadFirst(kindQ: TicketKind | '', yearQ: number | '') {
		const seq = ++requestSeq;
		loading = true;
		loadError = '';
		try {
			const page = await api.listTickets({
				kind: kindQ || undefined,
				year: yearQ || undefined
			});
			if (seq !== requestSeq) return;
			items = page.items;
			nextCursor = page.nextCursor;
		} catch {
			if (seq !== requestSeq) return;
			loadError = '票夹加载失败，请稍后重试';
		} finally {
			if (seq === requestSeq) loading = false;
		}
	}

	async function loadMore() {
		if (!nextCursor || loading || loadingMore) return;
		const seq = requestSeq;
		loadingMore = true;
		try {
			const page = await api.listTickets({
				kind: kind || undefined,
				year: year || undefined,
				cursor: nextCursor
			});
			if (seq !== requestSeq) return;
			items = [...items, ...page.items];
			nextCursor = page.nextCursor;
		} catch {
			// 加载更多失败：保留按钮可重试，不打断已有列表
		} finally {
			loadingMore = false;
		}
	}

	// 筛选变化即重查首页
	$effect(() => {
		void loadFirst(kind, year);
	});

	// 触底自动加载更多（哨兵元素；失败后仍有按钮兜底）
	let sentinel = $state<HTMLElement | null>(null);

	$effect(() => {
		const el = sentinel;
		if (!el) return;
		const observer = new IntersectionObserver((entries) => {
			if (entries.some((entry) => entry.isIntersecting)) void loadMore();
		});
		observer.observe(el);
		return () => observer.disconnect();
	});

	/* ---------- 展示派生：隐藏待删除条目 + 时间线按月分组 ---------- */

	const visible = $derived(
		items.filter(
			(t) => t.id !== pendingDelete.current?.id && t.id !== pendingDelete.committedId
		)
	);

	interface MonthGroup {
		label: string;
		items: Ticket[];
	}

	const groups = $derived.by((): MonthGroup[] => {
		const result: MonthGroup[] = [];
		for (const ticket of visible) {
			const label = fmtMonth(ticket.eventTime);
			const last = result.at(-1);
			if (last && last.label === label) {
				last.items.push(ticket);
			} else {
				result.push({ label, items: [ticket] });
			}
		}
		return result;
	});

	const filtered = $derived(kind !== '' || year !== '');

	// Svelte JS transition 不受全局 CSS reduced-motion 约束，需自行判断
	const reducedMotion = () =>
		typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
	const dur = (ms: number) => (reducedMotion() ? 0 : ms);
</script>

<svelte:head>
	<title>票夹 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<div class="head-row">
		<div>
			<h1>票夹</h1>
			<p class="sub">电影 · 演出 · 门票 · 车票 · 机票</p>
		</div>
		<div class="view-toggle" role="group" aria-label="切换视图">
			<button
				type="button"
				class="toggle-btn"
				class:on={view === 'wall'}
				aria-pressed={view === 'wall'}
				onclick={() => (view = 'wall')}
			>
				卡片
			</button>
			<button
				type="button"
				class="toggle-btn"
				class:on={view === 'timeline'}
				aria-pressed={view === 'timeline'}
				onclick={() => (view = 'timeline')}
			>
				时间线
			</button>
		</div>
	</div>
</header>

<!-- 筛选：票型 chips + 年份 -->
<div class="filters">
	<div class="kind-chips" role="group" aria-label="按票型筛选">
		<button
			type="button"
			class="chip"
			class:on={kind === ''}
			aria-pressed={kind === ''}
			onclick={() => (kind = '')}
		>
			全部
		</button>
		{#each TICKET_KINDS as k (k)}
			<button
				type="button"
				class="chip"
				class:on={kind === k}
				style:--chip-color={KIND_META[k].color}
				aria-pressed={kind === k}
				onclick={() => (kind = kind === k ? '' : k)}
			>
				<span aria-hidden="true">{KIND_META[k].emoji}</span>
				{KIND_META[k].label}
			</button>
		{/each}
	</div>
	<label class="year">
		<span class="year-label">年份</span>
		<select class="year-select tnum" bind:value={year} aria-label="按年份筛选">
			<option value="">全部</option>
			{#each YEARS as y (y)}
				<option value={y}>{y}</option>
			{/each}
		</select>
	</label>
</div>

{#if loading}
	<!-- 骨架屏（design §4：首屏必配） -->
	<div class="list" aria-hidden="true">
		{#each [0, 1, 2] as i (i)}
			<div class="skeleton-card">
				<Skeleton lines={3} />
			</div>
		{/each}
	</div>
{:else if loadError}
	<EmptyState emoji="🌫️" title="加载失败" description={loadError} actionLabel="重试" onaction={() => void loadFirst(kind, year)} />
{:else if visible.length === 0}
	{#if filtered}
		<EmptyState
			emoji="🔍"
			title="没有符合筛选的票根"
			description="换个票型或年份试试"
			actionLabel="清除筛选"
			onaction={() => {
				kind = '';
				year = '';
			}}
		/>
	{:else}
		<EmptyState
			emoji="🎫"
			title="票夹还空着"
			description="看过的电影、坐过的车、逛过的展，都值得留一张票根"
			actionLabel="存第一张票根"
			onaction={() => void goto('/tickets/new')}
		/>
	{/if}
{:else if view === 'wall'}
	<!-- 卡片墙 -->
	<div class="list">
		{#each visible as ticket (ticket.id)}
			<TicketCard {ticket} />
		{/each}
	</div>
{:else}
	<!-- 时间线：按月分组 + 票型色点竖轨 -->
	<div class="timeline">
		{#each groups as group (group.label)}
			<section class="tl-group">
				<h2 class="tl-month tnum">{group.label}</h2>
				<div class="tl-items">
					{#each group.items as ticket (ticket.id)}
						<a class="tl-row" href="/tickets/{ticket.id}">
							<span class="tl-dot" style:--dot={KIND_META[ticket.kind].color} aria-hidden="true"
							></span>
							<span class="tl-text">
								<span class="tl-title">{ticket.title}</span>
								<span class="tl-sub tnum">
									{KIND_META[ticket.kind].label} · {fmtDate(ticket.eventTime)}
								</span>
							</span>
							<Amount cents={ticket.transaction.amountCents} direction="expense" />
						</a>
					{/each}
				</div>
			</section>
		{/each}
	</div>
{/if}

{#if !loading && !loadError && nextCursor}
	<div class="more">
		<button type="button" class="more-btn" disabled={loadingMore} onclick={() => void loadMore()}>
			{loadingMore ? '加载中…' : '加载更多'}
		</button>
		<span class="sentinel" bind:this={sentinel} aria-hidden="true"></span>
	</div>
{/if}

<!-- FAB：存一张新票根 -->
<a class="fab" href="/tickets/new" aria-label="添加票根">＋</a>

<!-- 删除撤销 toast（design §4：删除后 5 秒可撤销） -->
{#if pendingDelete.current}
	<div class="toast" role="status" transition:fade={{ duration: dur(150) }}>
		<span class="toast-text">已删除「{pendingDelete.current.title}」</span>
		<button type="button" class="toast-undo" onclick={() => pendingDelete.undo()}>撤销</button>
	</div>
{/if}

<style>
	.page-head {
		padding-block: 24px 8px;
	}

	.head-row {
		display: flex;
		align-items: flex-start;
		justify-content: space-between;
		gap: 12px;
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

	/* ---- 视图切换 ---- */
	.view-toggle {
		display: flex;
		padding: 2px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--surface);
	}

	.toggle-btn {
		min-height: 40px;
		padding: 0 12px;
		border: none;
		border-radius: calc(var(--radius-btn) - 2px);
		background: transparent;
		color: var(--ink-2);
		font-family: inherit;
		font-size: 0.75rem; /* 12 辅助 */
		cursor: pointer;
		transition:
			background-color var(--dur-fast) var(--ease),
			color var(--dur-fast) var(--ease);
	}

	.toggle-btn.on {
		background: color-mix(in srgb, var(--brand) 12%, transparent);
		color: var(--brand);
		font-weight: 600;
	}

	/* ---- 筛选 ---- */
	.filters {
		display: flex;
		align-items: center;
		gap: 8px;
		padding-block: 8px 16px;
	}

	.kind-chips {
		display: flex;
		gap: 8px;
		overflow-x: auto;
		flex: 1;
		min-width: 0;
		scrollbar-width: none;
		/* chips 可横滑（design §4 分类网格同款交互） */
		-webkit-overflow-scrolling: touch;
	}

	.kind-chips::-webkit-scrollbar {
		display: none;
	}

	.chip {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 4px;
		min-height: 44px; /* 触控目标 */
		padding: 0 12px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--surface);
		color: var(--ink-2);
		font-family: inherit;
		font-size: 0.75rem; /* 12 辅助 */
		cursor: pointer;
		transition:
			border-color var(--dur-fast) var(--ease),
			background-color var(--dur-fast) var(--ease),
			color var(--dur-fast) var(--ease);
	}

	.chip.on {
		border-color: var(--chip-color, var(--brand));
		background: color-mix(in srgb, var(--chip-color, var(--brand)) 12%, transparent);
		color: var(--ink);
		font-weight: 600;
	}

	.year {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}

	.year-label {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.year-select {
		min-height: 44px; /* 触控目标 */
		padding: 0 8px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--surface);
		color: var(--ink);
		font-family: inherit;
		font-size: 0.875rem; /* 14 正文 */
	}

	/* ---- 列表 ---- */
	.list {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.skeleton-card {
		padding: 16px;
		background: var(--surface);
		border-radius: var(--radius-ticket);
		box-shadow: var(--shadow-card);
	}

	/* ---- 时间线 ---- */
	.timeline {
		display: flex;
		flex-direction: column;
		gap: 24px;
	}

	.tl-month {
		margin: 0 0 8px;
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 600;
		color: var(--ink-2);
	}

	.tl-items {
		position: relative;
		display: flex;
		flex-direction: column;
	}

	/* 竖轨 */
	.tl-items::before {
		content: '';
		position: absolute;
		left: 5px;
		top: 8px;
		bottom: 8px;
		width: 2px;
		background: var(--line);
	}

	.tl-row {
		position: relative;
		display: flex;
		align-items: center;
		gap: 12px;
		min-height: 56px; /* 触控目标 */
		padding: 8px 12px 8px 24px;
		border-radius: var(--radius-card);
		color: inherit;
		text-decoration: none;
		transition: background-color var(--dur-fast) var(--ease);
	}

	.tl-row:active {
		background: var(--surface);
	}

	.tl-dot {
		position: absolute;
		left: 0;
		width: 12px;
		height: 12px;
		border-radius: 50%;
		background: var(--dot);
		box-shadow: 0 0 0 2px var(--bg);
	}

	.tl-text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.tl-title {
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 600;
		color: var(--ink);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.tl-sub {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	/* ---- 加载更多 ---- */
	.more {
		display: flex;
		justify-content: center;
		padding-block: 16px;
	}

	.more-btn {
		min-height: 44px; /* 触控目标 */
		padding: 0 24px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--surface);
		color: var(--ink-2);
		font-family: inherit;
		font-size: 0.875rem; /* 14 正文 */
		cursor: pointer;
	}

	.more-btn:disabled {
		opacity: 0.5;
		cursor: wait;
	}

	.sentinel {
		width: 1px;
		height: 1px;
	}

	/* ---- FAB（56px --brand，右下角，避开 TabBar 与安全区） ---- */
	.fab {
		position: fixed;
		z-index: 30;
		right: max(var(--page-inline), calc(50vw - 320px + var(--page-inline)));
		bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom) + 16px);
		display: grid;
		place-items: center;
		width: 56px;
		height: 56px;
		border-radius: 50%;
		background: var(--brand);
		color: var(--on-brand);
		font-size: 1.75rem;
		line-height: 1;
		text-decoration: none;
		box-shadow: var(--shadow-card);
		transition: transform var(--dur-fast) var(--ease);
	}

	.fab:active {
		transform: scale(0.94);
	}

	/* ---- 撤销 toast ---- */
	.toast {
		position: fixed;
		z-index: 35;
		left: 50%;
		transform: translateX(-50%);
		bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom) + 16px);
		display: flex;
		align-items: center;
		gap: 8px;
		max-width: calc(100vw - 32px);
		padding: 8px 8px 8px 16px;
		background: var(--surface);
		border: 1px solid var(--line);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
	}

	.toast-text {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.toast-undo {
		flex: none;
		min-height: 44px; /* 触控目标 */
		padding: 0 12px;
		border: none;
		border-radius: var(--radius-btn);
		background: transparent;
		color: var(--brand);
		font-family: inherit;
		font-size: 0.875rem;
		font-weight: 600;
		cursor: pointer;
	}
</style>
