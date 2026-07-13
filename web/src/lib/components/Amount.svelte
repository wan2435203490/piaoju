<script lang="ts">
	import type { Direction } from '$lib/api/types';
	import { signedAmount } from '$lib/utils/money';

	interface Props {
		/** 整数分（渲染层才除 100 —— conventions §1） */
		cents: number;
		direction: Direction;
		/** md 跟随上下文字号（列表行）/ lg 28 金额大数 / xl 34 月度汇总 */
		size?: 'md' | 'lg' | 'xl';
	}

	let { cents, direction, size = 'md' }: Props = $props();
</script>

<!-- 支出 --brand 前缀 -，收入 --accent 前缀 +，千分位两位小数（design §1） -->
<span class="amount {size}" class:income={direction === 'income'}>{signedAmount(cents, direction)}</span>

<style>
	.amount {
		font-variant-numeric: tabular-nums;
		font-weight: 600;
		color: var(--brand); /* expense 默认 */
		white-space: nowrap;
	}

	.income {
		color: var(--accent);
	}

	.lg {
		font-size: 1.75rem; /* 28 金额大数 */
	}

	.xl {
		font-size: 2.125rem; /* 34 月度汇总 */
	}
</style>
