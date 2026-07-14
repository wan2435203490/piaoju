<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { data } from '$lib/data';
	import type { Category, MonthlyStats, Ticket, TicketStats } from '$lib/api/types';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import FootprintChart from '$lib/components/year/FootprintChart.svelte';
	import MonthlyBars from '$lib/components/year/MonthlyBars.svelte';
	import OverviewCard from '$lib/components/year/OverviewCard.svelte';
	import SuperlativesCard from '$lib/components/year/Superlatives.svelte';
	import YearSwitcher from '$lib/components/year/YearSwitcher.svelte';
	import {
		extractPlaces,
		kindSummary,
		mergeCategoryStats,
		monthKeys,
		monthlySeries,
		parseYear,
		superlatives,
		yearOptions
	} from '$lib/components/year/data';

	/* ================= 年份（?year= 可深链，非法值回退当前年） ================= */

	const currentYear = new Date().getFullYear();
	const years = yearOptions(currentYear);
	const year = $derived(parseYear(page.url.searchParams.get('year'), currentYear));

	function switchYear(next: number) {
		void goto(`/year?year=${next}`, { replaceState: true, noScroll: true, keepFocus: true });
	}

	/* ================= 数据（只走 $lib/data：本地库优先，可离线） ================= */

	let tickets = $state<Ticket[]>([]);
	let ticketStats = $state<TicketStats | null>(null);
	let monthly = $state<(MonthlyStats | null)[]>([]);
	let categories = $state<Category[]>([]);
	let initialLoading = $state(true);
	let refetching = $state(false);
	let loadError = $state('');

	let requestSeq = 0;

	/** 全年票根：本地库不分页，在线时按游标翻完（上限兜底防坏游标死循环） */
	async function fetchAllTickets(y: number): Promise<Ticket[]> {
		const out: Ticket[] = [];
		let cursor: string | undefined;
		for (let i = 0; i < 20; i++) {
			const pageData = await data.listTickets({ year: y, limit: 100, cursor });
			out.push(...pageData.items);
			if (!pageData.nextCursor) break;
			cursor = pageData.nextCursor;
		}
		return out;
	}

	async function load(y: number) {
		const seq = ++requestSeq;
		if (tickets.length === 0 && !ticketStats) initialLoading = true;
		else refetching = true;
		loadError = '';

		try {
			// 12 个月并发取，单月失败降级成 null（数据层再降级成 0），不让整页崩
			const [list, stats, months, cats] = await Promise.all([
				fetchAllTickets(y),
				data.statsTickets(y).catch((): TicketStats | null => null),
				Promise.all(
					monthKeys(y).map((m) => data.statsMonthly(m).catch((): MonthlyStats | null => null))
				),
				data.listCategories().catch((): Category[] => [])
			]);
			if (seq !== requestSeq) return;
			tickets = list;
			ticketStats = stats;
			monthly = months;
			categories = cats;
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

	// 年份变化（含首次进入）即重取
	$effect(() => {
		const y = year;
		void load(y);
	});

	/* ================= 派生视图模型（纯函数，见 components/year/data.ts） ================= */

	const summary = $derived(kindSummary(ticketStats, tickets));
	const places = $derived(extractPlaces(tickets));
	const points = $derived(monthlySeries(year, monthly));
	const byCategory = $derived(mergeCategoryStats(monthly));
	const best = $derived(superlatives(tickets, byCategory));
	const hasTickets = $derived(summary.total > 0 || tickets.length > 0);
	const hasSpend = $derived(points.some((p) => p.cents > 0));
</script>

<svelte:head>
	<title>{year} 年度报告 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<h1>年度报告</h1>
	<p class="sub">这一年的票根、足迹与花销</p>
</header>

<!-- 筛选一行置顶，作用于下方所有卡片 -->
<YearSwitcher value={year} {years} onchange={switchYear} />

{#if initialLoading}
	<div class="skeletons" aria-label="加载中">
		<Skeleton lines={1} height="160px" />
		<Skeleton lines={1} height="240px" />
		<Skeleton lines={1} height="240px" />
	</div>
{:else if loadError}
	<EmptyState
		emoji="⚠️"
		title="加载失败"
		description={loadError}
		actionLabel="重试"
		onaction={() => void load(year)}
	/>
{:else if !hasTickets}
	<EmptyState
		emoji="🎫"
		title="{year} 年还没有票根"
		description="存一张票，这里就会长出你的年度报告"
		actionLabel="去建一张票"
		onaction={() => void goto('/tickets/new')}
	/>
{:else}
	<!-- 重取时保留旧渲染降透明度：不闪骨架、不跳版（dataviz 交互规范） -->
	<div class="content" class:refetching>
		<OverviewCard {year} {summary} />

		<section class="card">
			<h2 class="card-title">旅行足迹</h2>
			{#if places.length > 0}
				<p class="card-sub">
					这一年你到过 <b class="tnum">{places.length}</b> 个地方（门票城市、车站与机场）
				</p>
				<FootprintChart {places} />
			{:else}
				<p class="card-sub">今年的票里还没有地点信息 —— 门票填城市、车票/机票填出发到达，这里就会记下来。</p>
			{/if}
		</section>

		<section class="card">
			<h2 class="card-title">月度消费趋势</h2>
			<p class="card-sub">全年记账支出（含非票据消费）</p>
			{#if hasSpend}
				<MonthlyBars {points} />
			{:else}
				<p class="card-sub">这一年还没有支出记录。</p>
			{/if}
		</section>

		<SuperlativesCard {best} {categories} />
	</div>
{/if}

<style>
	.page-head {
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

	.refetching {
		opacity: 0.55;
		pointer-events: none;
	}

	.card {
		padding: 16px;
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
	}

	.card-title {
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
		margin: 0 0 4px;
	}

	.card-sub {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		margin: 0 0 12px;
	}

	.tnum {
		font-variant-numeric: tabular-nums;
	}
</style>
