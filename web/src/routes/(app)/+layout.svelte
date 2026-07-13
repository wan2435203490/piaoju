<script lang="ts">
	import TabBar from '$lib/components/TabBar.svelte';

	let { children } = $props();
</script>

<!-- 三主页（账本/票夹/我的）共用：内容区 + 底部 TabBar -->
<main class="page">
	{@render children()}
</main>
<TabBar />

<style>
	.page {
		max-width: 640px;
		margin-inline: auto;
		padding-inline: var(--page-inline); /* 页面左右留白 16px */
		/* 给固定 TabBar 让位（含底部安全区） */
		padding-bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom) + 16px);
	}
</style>
