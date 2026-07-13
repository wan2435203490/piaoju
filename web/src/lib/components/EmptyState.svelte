<script lang="ts">
	import Button from './Button.svelte';

	interface Props {
		emoji?: string;
		title: string;
		description?: string;
		/** 提供 actionLabel + onaction 才渲染按钮 */
		actionLabel?: string;
		onaction?: () => void;
	}

	let { emoji = '🗂️', title, description = '', actionLabel = '', onaction }: Props = $props();
</script>

<div class="empty" role="status">
	<div class="emoji" aria-hidden="true">{emoji}</div>
	<h2 class="title">{title}</h2>
	{#if description}
		<p class="desc">{description}</p>
	{/if}
	{#if actionLabel && onaction}
		<div class="action">
			<Button onclick={onaction}>{actionLabel}</Button>
		</div>
	{/if}
</div>

<style>
	.empty {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		padding: 48px 24px;
		text-align: center;
	}

	.emoji {
		font-size: 3rem;
		line-height: 1;
		margin-bottom: 8px;
	}

	.title {
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
		margin: 0;
	}

	.desc {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
		margin: 0;
		max-width: 32ch;
	}

	.action {
		margin-top: 16px;
	}
</style>
