<script lang="ts">
	import Amount from '$lib/components/Amount.svelte';
	import type { Category, Transaction } from '$lib/api/types';
	import { PAYMENT_LABELS, timeHM } from './format';

	interface Props {
		tx: Transaction;
		/** 交易所属分类（页面从 listCategories 结果里查好传入） */
		category?: Category;
		/** 离线队列尚未同步 → 「待同步」小圆点（design §4） */
		pending?: boolean;
		onclick?: () => void;
	}

	let { tx, category, pending = false, onclick }: Props = $props();
</script>

<button type="button" class="row" {onclick}>
	<span class="icon" aria-hidden="true">{category?.icon ?? '📦'}</span>
	<span class="main">
		<span class="title">{tx.note || category?.name || '未分类'}</span>
		<span class="sub">
			{timeHM(tx.occurredAt)} · {PAYMENT_LABELS[tx.paymentMethod]}{tx.ticketId ? ' · 🎫票根' : ''}
		</span>
	</span>
	<span class="right">
		<Amount cents={tx.amountCents} direction={tx.direction} />
		{#if pending}
			<span class="dot" role="status" aria-label="待同步"></span>
		{/if}
	</span>
</button>

<style>
	.row {
		display: flex;
		align-items: center;
		gap: 12px;
		width: 100%;
		min-height: 56px; /* 触控目标 ≥ 44px */
		padding: 8px 12px;
		border: none;
		background: transparent;
		font-family: inherit;
		text-align: left;
		cursor: pointer;
		transition: background-color var(--dur-fast) var(--ease);
	}

	.row:active {
		background: var(--bg);
	}

	.icon {
		display: grid;
		place-items: center;
		flex: none;
		width: 40px;
		height: 40px;
		border-radius: 50%;
		background: var(--bg);
		font-size: 1.25rem;
		line-height: 1;
	}

	.main {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.title {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.sub {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		font-variant-numeric: tabular-nums;
	}

	.right {
		display: flex;
		align-items: center;
		gap: 6px;
		flex: none;
	}

	.dot {
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--brand);
	}
</style>
