<script lang="ts">
	import Amount from '$lib/components/Amount.svelte';
	import { KIND_META } from '$lib/components/ticket/kinds';
	import type { KindSummary } from './data';

	interface Props {
		year: number;
		summary: KindSummary;
	}

	let { year, summary }: Props = $props();

	/** 概览是「几个数字」，不是图 —— KPI 行（dataviz：a handful of headline numbers → stat tiles） */
	const tiles = $derived(summary.byKind.filter((k) => k.count > 0));
</script>

<section class="card" aria-labelledby="overview-title">
	<h2 class="card-title" id="overview-title">{year} 年，你收藏了</h2>

	<div class="hero">
		<div class="hero-main">
			<span class="hero-value tnum">{summary.total}</span>
			<span class="hero-unit">张票</span>
		</div>
		<div class="hero-side">
			<span class="hero-label">票据总花费</span>
			<Amount cents={summary.totalCents} direction="expense" size="lg" />
		</div>
	</div>

	{#if tiles.length > 0}
		<ul class="tiles">
			{#each tiles as k (k.kind)}
				{@const meta = KIND_META[k.kind]}
				<li class="tile">
					<!-- 票型色标：色条 + emoji + 文字，身份不靠色相单打（a11y） -->
					<span class="bar" style:background={meta.color} aria-hidden="true"></span>
					<span class="emoji" aria-hidden="true">{meta.emoji}</span>
					<span class="tile-body">
						<span class="tile-count"><b class="tnum">{k.count}</b> 场{meta.label}</span>
						<span class="tile-cents"><Amount cents={k.cents} direction="expense" /></span>
					</span>
				</li>
			{/each}
		</ul>
	{/if}
</section>

<style>
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

	.hero {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: 16px;
		padding-bottom: 16px;
		border-bottom: 1px solid var(--line);
	}

	.hero-main {
		display: flex;
		align-items: baseline;
		gap: 4px;
	}

	.hero-value {
		font-size: 2.125rem; /* 34 月度汇总（本页最大数，只此一个） */
		font-weight: 700;
		line-height: 1.25;
		color: var(--ink);
	}

	.hero-unit {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
	}

	.hero-side {
		display: flex;
		flex-direction: column;
		align-items: flex-end;
		gap: 4px;
	}

	.hero-label {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
		gap: 8px;
		list-style: none;
		margin: 16px 0 0;
		padding: 0;
	}

	.tile {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 44px;
		padding: 8px;
		border-radius: var(--radius-btn);
		background: var(--bg);
	}

	.bar {
		flex: none;
		width: 4px; /* 票型色条（票根视觉语言） */
		align-self: stretch;
		border-radius: 2px;
	}

	.emoji {
		flex: none;
		font-size: 1rem;
		line-height: 1;
	}

	.tile-body {
		display: flex;
		flex-direction: column;
		min-width: 0;
	}

	.tile-count {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
	}

	.tile-count b {
		font-weight: 700;
	}

	.tile-cents {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.tnum {
		font-variant-numeric: tabular-nums;
	}
</style>
