<script lang="ts">
	import Amount from '$lib/components/Amount.svelte';
	import { formatCents } from '$lib/utils/money';

	interface Props {
		expenseCents: number;
		incomeCents: number;
	}

	let { expenseCents, incomeCents }: Props = $props();

	const balanceCents = $derived(incomeCents - expenseCents);
</script>

<!-- 月度收支头卡：支出为主数（34 月度汇总档），收入/结余为辅 -->
<section class="card" aria-label="本月收支汇总">
	<div class="main">
		<p class="label">本月支出</p>
		<Amount cents={expenseCents} direction="expense" size="xl" />
	</div>
	<dl class="side">
		<div class="row">
			<dt>收入</dt>
			<dd><Amount cents={incomeCents} direction="income" /></dd>
		</div>
		<div class="row">
			<dt>结余</dt>
			<dd class="balance tnum">¥{formatCents(balanceCents)}</dd>
		</div>
	</dl>
</section>

<style>
	.card {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: 16px;
		padding: 16px;
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
	}

	.label {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		margin: 0 0 4px;
	}

	.side {
		display: flex;
		flex-direction: column;
		gap: 4px;
		margin: 0;
	}

	.row {
		display: flex;
		align-items: baseline;
		justify-content: flex-end;
		gap: 8px;
	}

	dt {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	dd {
		margin: 0;
		font-size: 0.875rem; /* 14 正文 */
	}

	.balance {
		font-weight: 600;
		color: var(--ink);
	}
</style>
