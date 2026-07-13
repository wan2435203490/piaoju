<script lang="ts">
	/**
	 * 高铁/飞机票卡（design §2 布局特化）：
	 * 出发地 → 到达地 大字横排，中间虚线 + 交通工具图标，
	 * 车次/航班号在头行，站点下方对齐出发/到达时刻。
	 */
	import type { Ticket } from '$lib/api/types';
	import Amount from '$lib/components/Amount.svelte';
	import TicketShell from './TicketShell.svelte';
	import { KIND_META, fmtDate, fmtTime } from './kinds';

	interface Props {
		ticket: Ticket<'train'> | Ticket<'flight'>;
		href?: string;
	}

	let { ticket, href }: Props = $props();

	interface Route {
		no: string;
		carrier: string;
		from: string;
		to: string;
		depart: string;
		arrive: string;
		seatClass: string;
	}

	const route = $derived.by((): Route => {
		if (ticket.kind === 'train') {
			const e = ticket.extra;
			return {
				no: e.trainNo,
				carrier: '',
				from: e.fromStation,
				to: e.toStation,
				depart: e.departTime,
				arrive: e.arriveTime,
				seatClass: e.seatClass
			};
		}
		const e = ticket.extra;
		return {
			no: e.flightNo,
			carrier: e.airline,
			from: e.fromAirport,
			to: e.toAirport,
			depart: e.departTime,
			arrive: e.arriveTime,
			seatClass: e.cabin
		};
	});

	const kindMeta = $derived(KIND_META[ticket.kind]);
	const subLine = $derived(
		[fmtDate(ticket.eventTime), ticket.seat, route.seatClass]
			.filter((p) => p !== '')
			.join(' · ')
	);
</script>

<TicketShell kind={ticket.kind} {href} rating={ticket.rating} memo={ticket.memo}>
	<div class="top">
		<span class="no tnum">
			<span aria-hidden="true">{kindMeta.emoji}</span>
			{route.no || kindMeta.label}
		</span>
		{#if route.carrier}
			<span class="carrier">{route.carrier}</span>
		{/if}
		<span class="amount"><Amount cents={ticket.transaction.amountCents} direction="expense" /></span>
	</div>

	{#if route.from && route.to}
		<div class="route">
			<div class="stop">
				<strong class="station">{route.from}</strong>
				{#if route.depart}
					<span class="time tnum">{fmtTime(route.depart)}</span>
				{/if}
			</div>
			<div class="track" aria-hidden="true">
				<span class="craft">{kindMeta.emoji}</span>
			</div>
			<div class="stop to">
				<strong class="station">{route.to}</strong>
				{#if route.arrive}
					<span class="time tnum">{fmtTime(route.arrive)}</span>
				{/if}
			</div>
		</div>
	{:else}
		<h3 class="title">{ticket.title}</h3>
	{/if}

	{#if subLine}
		<p class="sub tnum">{subLine}</p>
	{/if}
</TicketShell>

<style>
	.top {
		display: flex;
		align-items: center;
		gap: 8px;
	}

	.no {
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 600;
		color: var(--ink);
	}

	.carrier {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.amount {
		margin-left: auto;
	}

	.route {
		display: flex;
		align-items: flex-start;
		gap: 12px;
		margin-top: 12px;
	}

	.stop {
		display: flex;
		flex-direction: column;
		gap: 4px;
		min-width: 0;
	}

	.stop.to {
		align-items: flex-end;
		text-align: right;
	}

	.station {
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 700;
		color: var(--ink);
		line-height: 1.25;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		max-width: 100%;
	}

	.time {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	/* 中间虚线航迹 + 居中交通工具图标 */
	.track {
		position: relative;
		flex: 1;
		align-self: stretch;
		min-width: 48px;
	}

	.track::before {
		content: '';
		position: absolute;
		left: 0;
		right: 0;
		top: 0.75rem;
		border-top: 1px dashed var(--line);
	}

	.craft {
		position: absolute;
		left: 50%;
		top: 0.75rem;
		transform: translate(-50%, -50%);
		background: var(--surface);
		padding: 0 4px;
		font-size: 0.875rem;
		line-height: 1;
	}

	.title {
		margin: 12px 0 0;
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
	}

	.sub {
		margin: 12px 0 0;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}
</style>
