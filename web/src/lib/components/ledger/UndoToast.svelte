<script lang="ts">
	import { fly } from 'svelte/transition';
	import { motionDur } from './format';

	interface Props {
		/** 可 bind:open；到时自动关闭 */
		open?: boolean;
		message: string;
		/** 提供 actionLabel + onaction 才渲染操作按钮（如「撤销」） */
		actionLabel?: string;
		onaction?: () => void;
		/** 自动关闭毫秒数（design §4：删除后 5 秒可撤销） */
		duration?: number;
	}

	let { open = $bindable(false), message, actionLabel = '', onaction, duration = 5000 }: Props = $props();

	$effect(() => {
		if (!open) return;
		const timer = setTimeout(() => {
			open = false;
		}, duration);
		return () => clearTimeout(timer);
	});
</script>

{#if open}
	<div class="toast" role="status" transition:fly={{ y: 16, duration: motionDur(150) }}>
		<span class="msg">{message}</span>
		{#if actionLabel && onaction}
			<button type="button" class="action" onclick={onaction}>{actionLabel}</button>
		{/if}
	</div>
{/if}

<style>
	.toast {
		position: fixed;
		left: var(--page-inline);
		right: var(--page-inline);
		bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom) + 16px);
		z-index: 45;
		display: flex;
		align-items: center;
		gap: 8px;
		max-width: 480px;
		margin-inline: auto;
		padding: 4px 4px 4px 16px;
		background: var(--surface);
		border: 1px solid var(--line);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
	}

	.msg {
		flex: 1;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
	}

	.action {
		flex: none;
		min-height: 44px; /* 触控目标 */
		padding: 0 12px;
		border: none;
		background: transparent;
		border-radius: var(--radius-btn);
		font-family: inherit;
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--brand);
		cursor: pointer;
		transition: background-color var(--dur-fast) var(--ease);
	}

	.action:active {
		background: var(--bg);
	}
</style>
