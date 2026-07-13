<script lang="ts">
	import { goto } from '$app/navigation';
	import EmptyState from '$lib/components/EmptyState.svelte';
</script>

<svelte:head>
	<title>登录 · 拾光票局</title>
</svelte:head>

<h1 class="title">登录</h1>

<!-- W4 施工区：邮箱/密码表单 + client.login + token 持久化 -->
<EmptyState
	emoji="🔐"
	title="登录还没接线"
	description="账号体系正在接入（W4）；mock 模式下各页可直接浏览"
	actionLabel="还没有账号？去注册"
	onaction={() => goto('/auth/register')}
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
