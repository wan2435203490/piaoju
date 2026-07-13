<script lang="ts">
	/**
	 * 认证表单专用输入（auth/ 内部组件）。
	 * 校验态：error 非空 → 红描边 + aria-invalid + 下方错误文案（role=alert）。
	 * 若日后其他模块也要用，应由主线程提升到 $lib/components。
	 */
	import type { HTMLInputAttributes } from 'svelte/elements';

	interface Props {
		id: string;
		label: string;
		type?: 'text' | 'email' | 'password';
		value?: string;
		placeholder?: string;
		autocomplete?: HTMLInputAttributes['autocomplete'];
		/** 非空即错误态；优先于 hint 展示 */
		error?: string;
		/** 辅助说明（无错误时展示） */
		hint?: string;
		onblur?: () => void;
		oninput?: () => void;
	}

	let {
		id,
		label,
		type = 'text',
		value = $bindable(''),
		placeholder = '',
		autocomplete,
		error = '',
		hint = '',
		onblur,
		oninput
	}: Props = $props();
</script>

<div class="field">
	<label class="label" for={id}>{label}</label>
	<input
		{id}
		{type}
		{placeholder}
		{autocomplete}
		class="input"
		class:invalid={!!error}
		{value}
		oninput={(event) => {
			value = event.currentTarget.value;
			oninput?.();
		}}
		{onblur}
		aria-invalid={error ? true : undefined}
		aria-describedby={error ? `${id}-error` : hint ? `${id}-hint` : undefined}
	/>
	{#if error}
		<p class="error" id="{id}-error" role="alert">{error}</p>
	{:else if hint}
		<p class="hint" id="{id}-hint">{hint}</p>
	{/if}
</div>

<style>
	.field {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.label {
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 500;
		color: var(--ink);
	}

	.input {
		width: 100%;
		min-height: 48px; /* 触控目标 ≥ 44px；16px 字号防 iOS 聚焦缩放 */
		padding: 0 12px;
		background: var(--surface);
		color: var(--ink);
		border: 1px solid var(--line);
		border-radius: var(--radius-btn); /* 输入与按钮同 10px */
		font-family: inherit;
		font-size: 1rem; /* 16 强调 */
		transition: border-color var(--dur-fast) var(--ease);
	}

	.input::placeholder {
		color: var(--ink-2);
	}

	.input:focus {
		border-color: var(--brand);
	}

	.input.invalid {
		border-color: var(--danger);
	}

	.error {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--danger);
		margin: 0;
	}

	.hint {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		margin: 0;
	}
</style>
