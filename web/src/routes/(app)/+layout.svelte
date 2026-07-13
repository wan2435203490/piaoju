<script lang="ts">
	import { onMount } from 'svelte';
	import TabBar from '$lib/components/TabBar.svelte';
	import { initOffline } from '$lib/db/outbox';
	import { loadUser } from '../auth/session';

	let { children } = $props();

	// 登录态下的应用外壳：打开该用户的本地库、切到离线队列、启动同步调度。
	// 库名按 userId 隔离（换号不串数据）。mock 模式与 IndexedDB 不可用时是 no-op，
	// 写操作自动回退在线直写（见 db/outbox.ts）。
	onMount(() => {
		const user = loadUser();
		if (user) void initOffline(user.id);
	});
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
