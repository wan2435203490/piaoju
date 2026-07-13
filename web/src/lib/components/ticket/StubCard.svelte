<script lang="ts">
	/**
	 * 通用票根卡（movie/show/attraction/other）：
	 * 标题 + 金额 / 场馆·票型专属信息 / 时间·座位，撕票线下评分脚注。
	 * 无照片时正文左侧渲染票型图标 + 色块底纹（不留空白灰框，design §2）。
	 */
	import type { Ticket } from '$lib/api/types';
	import Amount from '$lib/components/Amount.svelte';
	import TicketShell from './TicketShell.svelte';
	import { KIND_META, fmtDateTime, stubMeta } from './kinds';

	interface Props {
		ticket: Ticket;
		href?: string;
	}

	let { ticket, href }: Props = $props();

	const meta = $derived(stubMeta(ticket));
	const photo = $derived(ticket.attachments[0]);
	const kindMeta = $derived(KIND_META[ticket.kind]);
	const timeLine = $derived(
		`${fmtDateTime(ticket.eventTime)}${ticket.seat ? ` · ${ticket.seat}` : ''}`
	);
</script>

<TicketShell
	kind={ticket.kind}
	{href}
	rating={ticket.rating}
	memo={ticket.memo}
	photoUrl={photo?.thumbUrl ?? ''}
	photoAlt={photo ? `${ticket.title} 票面照片` : ''}
>
	<div class="head">
		{#if !photo}
			<span class="tile" style:--tile-color={kindMeta.color} aria-hidden="true">
				{kindMeta.emoji}
			</span>
		{/if}
		<div class="text">
			<h3 class="title">{ticket.title}</h3>
			{#if meta}
				<p class="meta">{meta}</p>
			{/if}
			<p class="meta tnum">{timeLine}</p>
		</div>
		<Amount cents={ticket.transaction.amountCents} direction="expense" />
	</div>
</TicketShell>

<style>
	.head {
		display: flex;
		align-items: flex-start;
		gap: 12px;
	}

	/* 票型图标 + 色块底纹（无照片时） */
	.tile {
		flex: none;
		display: grid;
		place-items: center;
		width: 44px;
		height: 44px;
		border-radius: var(--radius-card);
		background: color-mix(in srgb, var(--tile-color) 14%, transparent);
		font-size: 1.25rem;
		line-height: 1;
	}

	.text {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.title {
		margin: 0;
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.meta {
		margin: 0;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
</style>
