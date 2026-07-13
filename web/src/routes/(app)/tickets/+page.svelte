<script lang="ts">
	import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<svelte:head>
	<title>票夹 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<h1>票夹</h1>
	<p class="sub">电影 · 演出 · 门票 · 车票 · 机票</p>
</header>

<!-- W3 施工区：票根卡片墙/时间线、kind/年份筛选、票详情与表单 -->
<EmptyState
	emoji="🎫"
	title="票夹还空着"
	description="看过的电影、坐过的车、逛过的展，都值得留一张票根"
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
