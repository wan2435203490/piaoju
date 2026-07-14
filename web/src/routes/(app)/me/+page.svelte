<script lang="ts">
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import Button from '$lib/components/Button.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import { teardownOffline } from '$lib/db/outbox';
	import { clearUser, hasSession, loadUser } from '../../auth/session';

	// User 只随 login/register 下发（契约无 GET /me），登录时已缓存到本地
	const user = loadUser();
	const loggedIn = user !== null && hasSession();

	const currentYear = new Date().getFullYear();

	const joined = (() => {
		if (!user) return '';
		const date = new Date(user.createdAt);
		if (Number.isNaN(date.getTime())) return '';
		return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: 'long' }).format(date);
	})();

	/* ---- 深色模式指示：跟随系统（design §5）。手动切换需改壳层，见交付说明 ---- */
	let prefersDark = $state(false);
	$effect(() => {
		const mq = window.matchMedia('(prefers-color-scheme: dark)');
		prefersDark = mq.matches;
		const onChange = (event: MediaQueryListEvent) => (prefersDark = event.matches);
		mq.addEventListener('change', onChange);
		return () => mq.removeEventListener('change', onChange);
	});

	/* ---- 退出登录：两步确认（3 秒内再点一次），成功/失败都回登录页 ---- */
	let confirmingLogout = $state(false);
	let loggingOut = $state(false);
	let confirmTimer: ReturnType<typeof setTimeout> | undefined;

	function onLogoutClick() {
		if (!confirmingLogout) {
			confirmingLogout = true;
			clearTimeout(confirmTimer);
			confirmTimer = setTimeout(() => (confirmingLogout = false), 3000);
			return;
		}
		void doLogout();
	}

	async function doLogout() {
		clearTimeout(confirmTimer);
		loggingOut = true;
		try {
			// client 内部无论请求成败都会清空本地 token（tokens.ts）
			await api.logout();
		} catch {
			// 网络失败也继续：本地 token 已清，登出对用户已生效
		}
		// 停同步调度、关本地库（数据留着——同一用户再登录可复用缓存）
		await teardownOffline();
		clearUser();
		await goto('/auth/login');
	}
</script>

<svelte:head>
	<title>我的 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<h1>我的</h1>
	<p class="sub">账号 · 偏好 · 数据</p>
</header>

{#if user && loggedIn}
	<section class="card profile" aria-label="账号信息">
		<div class="avatar" aria-hidden="true">{user.nickname.slice(0, 1) || '票'}</div>
		<div class="who">
			<p class="nick">{user.nickname}</p>
			<p class="mail">{user.email}</p>
		</div>
		{#if joined}
			<span class="since">{joined}加入</span>
		{/if}
	</section>

	<section class="card" aria-label="偏好与数据">
		<div class="row">
			<div class="row-main">
				<span class="row-label">深色模式</span>
				<span class="row-sub">跟随系统外观自动切换</span>
			</div>
			<span class="row-value" role="status">{prefersDark ? '🌙 深色' : '☀️ 浅色'}</span>
		</div>

		<!-- 年度报告（Wave 6）：票型概览 · 城市足迹 · 月度趋势 · 年度之最 -->
		<a class="row link-row" href="/year">
			<div class="row-main">
				<span class="row-label">{currentYear} 年度报告</span>
				<span class="row-sub">看过的场 · 去过的城 · 花掉的钱</span>
			</div>
			<span class="row-value" aria-hidden="true">›</span>
		</a>

		<!-- 账单导入（W6，PROTOCOL §6.2）：微信/支付宝 CSV → 预览核对 → 走 outbox 写入 -->
		<a class="row link-row" href="/import">
			<div class="row-main">
				<span class="row-label">导入账单</span>
				<span class="row-sub">微信 / 支付宝 CSV · 自动分类与查重</span>
			</div>
			<span class="row-value" aria-hidden="true">›</span>
		</a>

		<div class="row disabled" aria-disabled="true">
			<div class="row-main">
				<span class="row-label">导出数据</span>
				<span class="row-sub">账本 CSV · 票夹存档</span>
			</div>
			<span class="chip">即将上线</span>
		</div>
	</section>

	<div class="logout">
		<Button
			block
			variant={confirmingLogout ? 'danger' : 'ghost'}
			loading={loggingOut}
			onclick={onLogoutClick}
		>
			{loggingOut ? '正在退出…' : confirmingLogout ? '再点一次确认退出' : '退出登录'}
		</Button>
	</div>
{:else}
	<EmptyState
		emoji="👤"
		title="还未登录"
		description="登录后你的账本与票夹将在多端同步"
		actionLabel="去登录"
		onaction={() => goto('/auth/login')}
	/>
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

	.card {
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
		margin-top: 16px;
	}

	/* ---- 账号卡 ---- */
	.profile {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 16px;
	}

	.avatar {
		width: 48px;
		height: 48px;
		flex: none;
		display: flex;
		align-items: center;
		justify-content: center;
		border-radius: 50%;
		background: color-mix(in srgb, var(--brand) 12%, var(--surface));
		color: var(--brand);
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 700;
	}

	.who {
		min-width: 0;
	}

	.nick {
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
		margin: 0;
	}

	.mail {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		margin: 4px 0 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.since {
		margin-left: auto;
		flex: none;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	/* ---- 设置行 ---- */
	.row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		min-height: 44px; /* 触控目标 */
		padding: 12px 16px;
	}

	.row + .row {
		border-top: 1px solid var(--line);
	}

	.row-main {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}

	.row-label {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
	}

	.row-sub {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.row-value {
		flex: none;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
	}

	/* 可点设置行（导入账单入口） */
	.link-row {
		text-decoration: none;
		color: inherit;
		transition: background-color var(--dur-fast) var(--ease);
	}

	.link-row:active {
		background: var(--bg);
	}

	.row.disabled .row-label,
	.row.disabled .row-sub {
		opacity: 0.6;
	}

	.chip {
		flex: none;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		border: 1px solid var(--line);
		border-radius: var(--radius-chip);
		padding: 4px 8px;
	}

	.logout {
		margin-top: 24px;
	}
</style>
