<script lang="ts">
	/**
	 * 票型表单输入行（ticket 模块内部组件）。
	 * 样式对齐 auth/FormField（surface 底 + line 描边 + 48px 触控高度）；
	 * FormField 属 auth 目录私有，若主线程日后提升公共版应替换本组件。
	 */
	interface Props {
		id: string;
		label: string;
		type?: 'text' | 'datetime-local' | 'textarea';
		value?: string;
		placeholder?: string;
		/** 数字金额等场景弹数字键盘 */
		inputmode?: 'text' | 'decimal';
		/** 非空即错误态 */
		error?: string;
	}

	let {
		id,
		label,
		type = 'text',
		value = $bindable(''),
		placeholder = '',
		inputmode = 'text',
		error = ''
	}: Props = $props();
</script>

<div class="field">
	<label class="label" for={id}>{label}</label>
	{#if type === 'textarea'}
		<textarea
			{id}
			{placeholder}
			class="input area"
			class:invalid={!!error}
			rows="3"
			{value}
			oninput={(event) => (value = event.currentTarget.value)}
			aria-invalid={error ? true : undefined}
			aria-describedby={error ? `${id}-error` : undefined}
		></textarea>
	{:else}
		<input
			{id}
			{type}
			{placeholder}
			{inputmode}
			class="input"
			class:invalid={!!error}
			{value}
			oninput={(event) => (value = event.currentTarget.value)}
			aria-invalid={error ? true : undefined}
			aria-describedby={error ? `${id}-error` : undefined}
		/>
	{/if}
	{#if error}
		<p class="error" id="{id}-error" role="alert">{error}</p>
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
		border-radius: var(--radius-btn);
		font-family: inherit;
		font-size: 1rem; /* 16 强调 */
		transition: border-color var(--dur-fast) var(--ease);
	}

	.area {
		padding: 12px;
		line-height: 1.5;
		resize: vertical;
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
</style>
