<script lang="ts">
	interface Props {
		value: number;
		years: number[];
		onchange: (year: number) => void;
	}

	let { value, years, onchange }: Props = $props();
</script>

<!-- 筛选一行置顶，作用于下方所有卡片（dataviz 交互规范：filters 不进图卡） -->
<div class="switcher" role="group" aria-label="选择年份">
	{#each years as year (year)}
		<button
			type="button"
			class="year"
			class:active={year === value}
			aria-pressed={year === value}
			onclick={() => onchange(year)}
		>
			<span class="tnum">{year}</span>
		</button>
	{/each}
</div>

<style>
	.switcher {
		display: flex;
		gap: 8px;
		overflow-x: auto;
		padding: 4px 0 8px;
		scrollbar-width: none;
	}

	.switcher::-webkit-scrollbar {
		display: none;
	}

	.year {
		flex: none;
		min-height: 44px; /* 触控目标 ≥ 44px */
		padding-inline: 16px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--surface);
		color: var(--ink-2);
		font-family: inherit;
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 600;
		cursor: pointer;
		transition:
			background-color var(--dur-fast) var(--ease),
			color var(--dur-fast) var(--ease);
	}

	.year.active {
		background: var(--brand);
		border-color: var(--brand);
		color: var(--on-brand);
	}

	.tnum {
		font-variant-numeric: tabular-nums;
	}
</style>
