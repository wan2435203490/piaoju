<script lang="ts">
	import { goto } from '$app/navigation';
	import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<svelte:head>
	<title>注册 · 拾光票局</title>
</svelte:head>

<h1 class="title">注册</h1>

<!-- W4 施工区：邮箱/密码(≥8)/昵称表单 + client.register -->
<EmptyState
	emoji="✉️"
	title="注册还没接线"
	description="账号体系正在接入（W4）；mock 模式下各页可直接浏览"
	actionLabel="已有账号？去登录"
	onaction={() => goto('/auth/login')}
/>

<a class="skip" href="/ledger">先随便逛逛 →</a>

<style>
	.title {
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 700;
		color: var(--ink);
		margin: 0;
		text-align: center;
	}

	.skip {
		margin-top: auto;
		text-align: center;
		font-size: 0.875rem;
		color: var(--ink-2);
		text-decoration: none;
		padding: 12px; /* 触控目标 */
	}

	.skip:active {
		color: var(--brand);
	}
</style>
