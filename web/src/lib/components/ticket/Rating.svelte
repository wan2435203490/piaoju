<script lang="ts">
	interface Props {
		/** 0-5，0 = 未评（PROTOCOL §5） */
		value: number;
		/** 传入即为可交互模式（表单）；再点当前星级 = 清零 */
		onchange?: (value: number) => void;
		/** sm 票根卡脚注 / md 详情、表单 */
		size?: 'sm' | 'md';
	}

	let { value, onchange, size = 'sm' }: Props = $props();

	const stars = [1, 2, 3, 4, 5] as const;
</script>

{#if onchange}
	<div class="rating input {size}" role="radiogroup" aria-label="评分">
		{#each stars as n (n)}
			<button
				type="button"
				class="star"
				class:filled={n <= value}
				role="radio"
				aria-checked={value === n}
				aria-label="{n} 星"
				onclick={() => onchange(value === n ? 0 : n)}
			>
				{n <= value ? '★' : '☆'}
			</button>
		{/each}
	</div>
{:else}
	<span class="rating {size}" role="img" aria-label="评分 {value} / 5 星">
		{#each stars as n (n)}
			<span class="star" class:filled={n <= value} aria-hidden="true">{n <= value ? '★' : '☆'}</span>
		{/each}
	</span>
{/if}

<style>
	.rating {
		display: inline-flex;
		align-items: center;
		line-height: 1;
	}

	.star {
		color: var(--line);
		font-size: 0.875rem; /* 14 正文 */
	}

	.star.filled {
		color: var(--brand);
	}

	.md .star {
		font-size: 1.25rem; /* 20 标题 */
	}

	/* 可交互模式：触控目标 ≥ 44px */
	.input .star {
		display: grid;
		place-items: center;
		min-width: 44px;
		min-height: 44px;
		padding: 0;
		border: none;
		background: transparent;
		font-family: inherit;
		cursor: pointer;
		transition:
			color var(--dur-fast) var(--ease),
			transform var(--dur-fast) var(--ease);
	}

	.input .star:active {
		transform: scale(1.2);
	}
</style>
