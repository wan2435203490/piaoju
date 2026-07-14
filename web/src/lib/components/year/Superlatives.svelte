<script lang="ts">
	import type { Category } from '$lib/api/types';
	import Amount from '$lib/components/Amount.svelte';
	import Rating from '$lib/components/ticket/Rating.svelte';
	import { KIND_META, fmtDate } from '$lib/components/ticket/kinds';
	import { formatCents } from '$lib/utils/money';
	import { categoryLabel, ticketVenue, type Superlatives } from './data';

	interface Props {
		best: Superlatives;
		categories: Category[];
	}

	let { best, categories }: Props = $props();
</script>

<section class="card" aria-labelledby="best-title">
	<h2 class="card-title" id="best-title">年度之最</h2>

	<ul class="items">
		{#if best.priciest}
			{@const t = best.priciest}
			<li>
				<a class="item" href="/tickets/{t.id}">
					<span class="tag">💸 最贵的一张</span>
					<span class="main">
						<span class="kind" style:background={KIND_META[t.kind].color} aria-hidden="true"></span>
						<span class="title">{t.title}</span>
						<Amount cents={t.transaction.amountCents} direction="expense" />
					</span>
					<span class="meta">{fmtDate(t.eventTime)}{ticketVenue(t) ? ` · ${ticketVenue(t)}` : ''}</span>
				</a>
			</li>
		{/if}

		{#if best.topRated}
			{@const t = best.topRated}
			<li>
				<a class="item" href="/tickets/{t.id}">
					<span class="tag">⭐ 评分最高</span>
					<span class="main">
						<span class="kind" style:background={KIND_META[t.kind].color} aria-hidden="true"></span>
						<span class="title">{t.title}</span>
						<Rating value={t.rating} />
					</span>
					<span class="meta">{fmtDate(t.eventTime)}{t.memo ? ` · ${t.memo}` : ''}</span>
				</a>
			</li>
		{/if}

		{#if best.topVenue}
			<li>
				<div class="item">
					<span class="tag">📍 去得最多</span>
					<span class="main">
						<span class="title">{best.topVenue.name}</span>
						<span class="strong tnum">{best.topVenue.count} 次</span>
					</span>
					<span class="meta">这一年你最常出现的场馆</span>
				</div>
			</li>
		{/if}

		{#if best.topCategory}
			<li>
				<div class="item">
					<span class="tag">🏷️ 花钱最多的分类</span>
					<span class="main">
						<span class="title">{categoryLabel(categories, best.topCategory.categoryId)}</span>
						<span class="strong tnum">¥{formatCents(best.topCategory.cents)}</span>
					</span>
					<span class="meta tnum">{best.topCategory.count} 笔（含非票据消费）</span>
				</div>
			</li>
		{/if}
	</ul>
</section>

<style>
	.card {
		padding: 16px;
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
	}

	.card-title {
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
		margin: 0 0 12px;
	}

	.items {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.item {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-height: 44px; /* 触控目标 */
		padding: 8px;
		border-radius: var(--radius-btn);
		background: var(--bg);
		color: var(--ink);
		text-decoration: none;
		transition: opacity var(--dur-fast) var(--ease);
	}

	a.item:active {
		opacity: 0.7;
	}

	.tag {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.main {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.kind {
		flex: none;
		width: 4px;
		height: 16px;
		border-radius: 2px;
	}

	.title {
		flex: 1;
		min-width: 0;
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.strong {
		flex: none;
		font-size: 1rem;
		font-weight: 600;
		color: var(--ink);
	}

	.meta {
		font-size: 0.75rem;
		color: var(--ink-2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.tnum {
		font-variant-numeric: tabular-nums;
	}
</style>
