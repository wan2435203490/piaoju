<script lang="ts">
	import { page } from '$app/state';

	const tabs = [
		{ href: '/ledger', label: '账本', emoji: '📒' },
		{ href: '/tickets', label: '票夹', emoji: '🎫' },
		{ href: '/me', label: '我的', emoji: '👤' }
	] as const;

	const isActive = (href: string) => page.url.pathname.startsWith(href);
</script>

<nav class="tabbar">
	<div class="tabs">
		{#each tabs as tab (tab.href)}
			<a
				href={tab.href}
				class="tab"
				class:active={isActive(tab.href)}
				aria-current={isActive(tab.href) ? 'page' : undefined}
			>
				<span class="emoji" aria-hidden="true">{tab.emoji}</span>
				<span class="label">{tab.label}</span>
			</a>
		{/each}
	</div>
</nav>

<style>
	.tabbar {
		position: fixed;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 30;
		background: var(--surface);
		border-top: 1px solid var(--line);
		/* 固定元素必须处理安全区（design §4） */
		padding-bottom: env(safe-area-inset-bottom);
	}

	.tabs {
		display: flex;
		max-width: 640px;
		margin-inline: auto;
		height: var(--tabbar-height);
	}

	.tab {
		flex: 1;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		min-height: 44px; /* 触控目标 */
		text-decoration: none;
		color: var(--ink-2);
		transition: color var(--dur-fast) var(--ease);
	}

	.emoji {
		font-size: 1.25rem;
		line-height: 1;
		filter: grayscale(1);
		opacity: 0.75;
		transition:
			filter var(--dur-fast) var(--ease),
			opacity var(--dur-fast) var(--ease);
	}

	.label {
		font-size: 0.75rem; /* 12 辅助 */
	}

	.active {
		color: var(--brand); /* 选中态 --brand */
		font-weight: 600;
	}

	.active .emoji {
		filter: none;
		opacity: 1;
	}
</style>
