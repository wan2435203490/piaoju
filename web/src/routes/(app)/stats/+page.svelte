<script lang="ts">
	import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<svelte:head>
	<title>统计 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<h1>统计</h1>
	<p class="sub">收支概览 · 分类占比 · 日趋势</p>
</header>

<!-- W2 施工区：月收支汇总、Donut 分类占比、日趋势条图（自绘 SVG，先读 dataviz skill） -->
<EmptyState
	emoji="📊"
	title="本月还没有可统计的数据"
	description="记几笔账，这里会长出你的消费图表"
/>

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
</style>
