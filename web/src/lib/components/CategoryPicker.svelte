<script lang="ts">
	import type { Category } from '$lib/api/types';

	interface Props {
		/** 分类数据由调用方传入（mock 模式来自 api.listCategories → fixtures） */
		categories: Category[];
		/** 选中的 categoryId */
		selected?: number | null;
		onselect?: (category: Category) => void;
		columns?: number;
	}

	let { categories, selected = null, onselect, columns = 4 }: Props = $props();
</script>

<!-- 网格图标选择壳（快记面板 / 票型表单共用） -->
<div class="picker" role="listbox" aria-label="选择分类" style:--cols={columns}>
	{#each categories as category (category.id)}
		<button
			type="button"
			class="cell"
			class:selected={selected === category.id}
			role="option"
			aria-selected={selected === category.id}
			onclick={() => onselect?.(category)}
		>
			<span class="icon" aria-hidden="true">{category.icon}</span>
			<span class="name">{category.name}</span>
		</button>
	{/each}
</div>

<style>
	.picker {
		display: grid;
		grid-template-columns: repeat(var(--cols), 1fr);
		gap: 8px;
	}

	.cell {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 4px;
		min-height: 64px; /* 触控目标 ≥ 44px */
		padding: 8px 4px;
		border: 1px solid transparent;
		border-radius: var(--radius-btn);
		background: transparent;
		cursor: pointer;
		font-family: inherit;
		transition:
			background-color var(--dur-fast) var(--ease),
			border-color var(--dur-fast) var(--ease);
	}

	.cell:active {
		background: var(--bg);
	}

	.icon {
		display: grid;
		place-items: center;
		width: 40px;
		height: 40px;
		border-radius: 50%;
		background: var(--bg);
		font-size: 1.25rem;
		line-height: 1;
		transition: background-color var(--dur-fast) var(--ease);
	}

	.name {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.selected {
		border-color: var(--brand);
		background: color-mix(in srgb, var(--brand) 8%, transparent);
	}

	.selected .icon {
		background: color-mix(in srgb, var(--brand) 16%, transparent);
	}

	.selected .name {
		color: var(--brand);
		font-weight: 600;
	}
</style>
