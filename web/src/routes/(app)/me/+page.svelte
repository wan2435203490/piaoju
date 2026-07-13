<script lang="ts">
	import { goto } from '$app/navigation';
	import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<svelte:head>
	<title>我的 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<h1>我的</h1>
	<p class="sub">账号 · 偏好 · 数据</p>
</header>

<!-- W4 施工区：昵称卡、导出入口、深色模式指示、退出登录 -->
<EmptyState
	emoji="👤"
	title="还未登录"
	description="登录后你的账本与票夹将在多端同步"
	actionLabel="去登录"
	onaction={() => goto('/auth/login')}
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
