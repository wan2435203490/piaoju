<script lang="ts">
	import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<svelte:head>
	<title>账本 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<h1>账本</h1>
	<p class="sub">月度流水 · 快记</p>
</header>

<!-- W2 施工区：月切换、收支头卡、日分组流水、QuickAddSheet FAB -->
<EmptyState
	emoji="🧾"
	title="还没有一笔账"
	description="第一笔账只要 3 秒 —— 快记面板即将在这里出现"
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
