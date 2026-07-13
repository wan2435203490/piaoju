<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { data } from '$lib/data';
	import type { Category, MonthlyStats } from '$lib/api/types';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import MonthSwitcher from '$lib/components/ledger/MonthSwitcher.svelte';
	import MonthSummaryCard from '$lib/components/ledger/MonthSummaryCard.svelte';
	import DonutChart, { type DonutSlice } from '$lib/components/ledger/DonutChart.svelte';
	import TrendChart from '$lib/components/ledger/TrendChart.svelte';
	import { currentMonth } from '$lib/components/ledger/format';
	import { formatCents } from '$lib/utils/money';

	/* ================= 状态 ================= */

	let month = $state(currentMonth());
	let stats = $state<MonthlyStats | null>(null);
	let categories = $state<Category[]>([]);
	let initialLoading = $state(true);
	let refetching = $state(false);
	let loadError = $state('');
	/** 环形图选中切片（图例行与圆环共用） */
	let donutSelected = $state<number | null>(null);

	let requestSeq = 0;

	async function loadMonth(target: string, initial = false) {
		const seq = ++requestSeq;
		if (initial) initialLoading = true;
		else refetching = true;
		loadError = '';
		donutSelected = null;
		try {
			const monthly = await data.statsMonthly(target);
			if (seq !== requestSeq) return;
			stats = monthly;
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

	function switchMonth(next: string) {
		month = next;
		void loadMonth(next);
	}

	onMount(() => {
		void loadMonth(month, true);
		data
			.listCategories()
			.then((list) => (categories = list))
			.catch(() => {
				/* 分类名缺失时回退展示「分类 #id」 */
			});
	});

	/* ================= 派生：环形图切片 ================= */

	const categoryMap = $derived(new Map(categories.map((c) => [c.id, c])));

	/**
	 * 类别是「身份」→ 分类色板（dataviz categorical）。
	 * 用既有票型 token 按固定顺序做 5 个身份槽（已跑 validate_palette.js，
	 * light/dark 双面全过：CVD 最差相邻 ΔE 48+）；--kind-other（灰）只留给
	 * 折叠的「其余」槽 —— 灰 = 去强调，正是折叠语义。
	 */
	const PALETTE = [
		'var(--kind-movie)',
		'var(--kind-flight)',
		'var(--kind-attraction)',
		'var(--kind-show)',
		'var(--kind-train)'
	] as const;
	const FOLD_COLOR = 'var(--kind-other)';
	const FOLD_KEY = -1;

	/** donut ≤6 段：top5 + 尾部折叠（dataviz：不为多出的类生造色相） */
	const slices = $derived.by((): DonutSlice[] => {
		if (!stats) return [];
		const sorted = [...stats.byCategory].sort((a, b) => b.cents - a.cents);
		const top = sorted.slice(0, PALETTE.length);
		const rest = sorted.slice(PALETTE.length);
		const out: DonutSlice[] = top.map((s, i) => {
			const cat = categoryMap.get(s.categoryId);
			return {
				key: s.categoryId,
				label: cat ? `${cat.icon} ${cat.name}` : `分类 #${s.categoryId}`,
				cents: s.cents,
				count: s.count,
				color: PALETTE[i] ?? FOLD_COLOR
			};
		});
		if (rest.length > 0) {
			out.push({
				key: FOLD_KEY,
				label: `其余 ${rest.length} 类`,
				cents: rest.reduce((acc, s) => acc + s.cents, 0),
				count: rest.reduce((acc, s) => acc + s.count, 0),
				color: FOLD_COLOR
			});
		}
		return out;
	});

	const sliceSum = $derived(slices.reduce((acc, s) => acc + s.cents, 0));

	const hasData = $derived(
		stats != null && (stats.expenseCents > 0 || stats.incomeCents > 0 || stats.byCategory.length > 0)
	);
</script>

<svelte:head>
	<title>统计 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<h1>统计</h1>
	<p class="sub">收支概览 · 分类占比 · 日趋势</p>
</header>

<!-- 筛选一行置顶，作用于下方所有图卡（dataviz 交互规范） -->
<div class="month-row">
	<MonthSwitcher value={month} onchange={switchMonth} />
</div>

{#if initialLoading}
	<div class="skeletons" aria-label="加载中">
		<Skeleton lines={1} height="88px" />
		<Skeleton lines={1} height="260px" />
		<Skeleton lines={1} height="220px" />
	</div>
{:else if loadError}
	<EmptyState
		emoji="⚠️"
		title="加载失败"
		description={loadError}
		actionLabel="重试"
		onaction={() => void loadMonth(month, true)}
	/>
{:else if !hasData}
	<EmptyState
		emoji="📊"
		title="本月还没有可统计的数据"
		description="记几笔账，这里会长出你的消费图表"
		actionLabel="去记一笔"
		onaction={() => void goto('/ledger')}
	/>
{:else if stats}
	<div class="content" class:refetching>
		<MonthSummaryCard expenseCents={stats.expenseCents} incomeCents={stats.incomeCents} />

		{#if slices.length > 0}
			<section class="card">
				<h2 class="card-title">支出分类占比</h2>
				<DonutChart {slices} totalCents={stats.expenseCents} bind:selected={donutSelected} />
				<!-- 图例 + 表格视图二合一：身份不靠色相单打（图标/名称/数值同排） -->
				<ul class="legend">
					{#each slices as slice (slice.key)}
						<li>
							<button
								type="button"
								class="legend-row"
								class:dim={donutSelected != null && donutSelected !== slice.key}
								aria-pressed={donutSelected === slice.key}
								onclick={() => (donutSelected = donutSelected === slice.key ? null : slice.key)}
							>
								<span class="swatch" style:background={slice.color} aria-hidden="true"></span>
								<span class="legend-label">{slice.label}</span>
								<span class="legend-count tnum">{slice.count} 笔</span>
								<span class="legend-value tnum">¥{formatCents(slice.cents)}</span>
								<span class="legend-pct tnum"
									>{sliceSum > 0 ? ((slice.cents / sliceSum) * 100).toFixed(1) : '0.0'}%</span
								>
							</button>
						</li>
					{/each}
				</ul>
			</section>
		{/if}

		<section class="card">
			<h2 class="card-title">每日支出趋势</h2>
			<TrendChart {month} byDay={stats.byDay} />
		</section>
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

	/* 换月重取：保留旧图降透明度，不闪骨架、不跳版（dataviz 交互规范） */
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
		margin: 0 0 12px;
	}

	.legend {
		list-style: none;
		margin: 12px 0 0;
		padding: 0;
	}

	.legend-row {
		display: flex;
		align-items: center;
		gap: 8px;
		width: 100%;
		min-height: 44px; /* 触控目标 */
		padding: 0 4px;
		border: none;
		background: transparent;
		font-family: inherit;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
		cursor: pointer;
		transition:
			opacity var(--dur-fast) var(--ease),
			background-color var(--dur-fast) var(--ease);
	}

	.legend-row:active {
		background: var(--bg);
	}

	.legend-row.dim {
		opacity: 0.45;
	}

	.swatch {
		flex: none;
		width: 10px;
		height: 10px;
		border-radius: 3px;
	}

	.legend-label {
		flex: 1;
		min-width: 0;
		text-align: left;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.legend-count {
		flex: none;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.legend-value {
		flex: none;
		font-weight: 600;
	}

	.legend-pct {
		flex: none;
		width: 52px;
		text-align: right;
		font-size: 0.75rem;
		color: var(--ink-2);
	}
</style>
