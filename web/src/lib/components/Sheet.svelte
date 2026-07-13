<script lang="ts">
	import type { Snippet } from 'svelte';
	import { fade, fly } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';

	interface Props {
		/** 可 bind:open；关闭时同时回调 onclose */
		open?: boolean;
		title?: string;
		onclose?: () => void;
		children?: Snippet;
	}

	let { open = $bindable(false), title = '', onclose, children }: Props = $props();

	// Svelte JS transition 不受全局 CSS reduced-motion 规则约束，需自行判断
	const reducedMotion = () =>
		typeof window !== 'undefined' && window.matchMedia('(prefers-reduced-motion: reduce)').matches;
	const dur = (ms: number) => (reducedMotion() ? 0 : ms);

	function close() {
		open = false;
		onclose?.();
	}

	function onkeydown(event: KeyboardEvent) {
		if (event.key === 'Escape' && open) close();
	}

	// 弹层打开时锁定背景滚动
	$effect(() => {
		if (!open || typeof document === 'undefined') return;
		const prev = document.body.style.overflow;
		document.body.style.overflow = 'hidden';
		return () => {
			document.body.style.overflow = prev;
		};
	});
</script>

<svelte:window {onkeydown} />

{#if open}
	<div
		class="backdrop"
		transition:fade={{ duration: dur(250) }}
		onclick={close}
		aria-hidden="true"
	></div>
	<div
		class="sheet"
		role="dialog"
		aria-modal="true"
		aria-label={title || '底部面板'}
		transition:fly={{ y: 360, duration: dur(250), easing: cubicOut, opacity: 1 }}
	>
		<div class="grabber" aria-hidden="true"></div>
		{#if title}
			<h2 class="title">{title}</h2>
		{/if}
		{@render children?.()}
	</div>
{/if}

<style>
	.backdrop {
		position: fixed;
		inset: 0;
		background: var(--scrim);
		z-index: 40;
	}

	.sheet {
		position: fixed;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 41;
		max-width: 640px;
		margin-inline: auto;
		max-height: 85dvh;
		overflow-y: auto;
		background: var(--surface);
		border-radius: var(--radius-sheet) var(--radius-sheet) 0 0; /* sheet 顶部圆角 20 */
		box-shadow: var(--shadow-card); /* 暗色下自动变 --line 描边 */
		padding: 8px var(--page-inline) calc(16px + env(safe-area-inset-bottom));
	}

	.grabber {
		width: 36px;
		height: 4px;
		border-radius: var(--radius-chip);
		background: var(--line);
		margin: 4px auto 12px;
	}

	.title {
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
		margin: 0 0 12px;
		text-align: center;
	}
</style>
