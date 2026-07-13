<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		/** primary 实底主操作 / ghost 描边次操作 / danger 危险操作 */
		variant?: 'primary' | 'ghost' | 'danger';
		loading?: boolean;
		disabled?: boolean;
		type?: 'button' | 'submit' | 'reset';
		/** 占满整行（表单主按钮） */
		block?: boolean;
		onclick?: (event: MouseEvent) => void;
		children: Snippet;
	}

	let {
		variant = 'primary',
		loading = false,
		disabled = false,
		type = 'button',
		block = false,
		onclick,
		children
	}: Props = $props();
</script>

<button {type} class="btn {variant}" class:block disabled={disabled || loading} aria-busy={loading} {onclick}>
	{#if loading}
		<span class="spinner" aria-hidden="true"></span>
	{/if}
	<span class="label">{@render children()}</span>
</button>

<style>
	.btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		min-height: 44px; /* 触控目标 ≥ 44px */
		padding: 0 16px;
		border: 1px solid transparent;
		border-radius: var(--radius-btn);
		font-family: inherit;
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		line-height: 1.25;
		cursor: pointer;
		transition:
			transform var(--dur-fast) var(--ease),
			opacity var(--dur-fast) var(--ease),
			background-color var(--dur-fast) var(--ease);
		-webkit-user-select: none;
		user-select: none;
	}

	.btn:active:not(:disabled) {
		transform: scale(0.97);
	}

	.btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.primary {
		background: var(--brand);
		color: var(--on-brand);
	}

	.ghost {
		background: transparent;
		color: var(--brand);
		border-color: var(--line);
	}

	.danger {
		background: var(--danger);
		color: var(--on-danger);
	}

	.block {
		display: flex;
		width: 100%;
	}

	.spinner {
		width: 16px;
		height: 16px;
		flex: none;
		border: 2px solid currentColor;
		border-top-color: transparent;
		border-radius: 50%;
		animation: spin 0.8s linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
</style>
