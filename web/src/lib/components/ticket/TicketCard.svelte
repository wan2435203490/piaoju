<script lang="ts">
	/**
	 * TicketCard —— 按 kind 分派布局（design skill §3）：
	 * train/flight → TransitCard（站点横排）；其余 → StubCard（通用票根）。
	 * 页面统一从这里取卡片，不感知具体布局。
	 */
	import type { Ticket } from '$lib/api/types';
	import { isKind } from '$lib/api/types';
	import StubCard from './StubCard.svelte';
	import TransitCard from './TransitCard.svelte';

	interface Props {
		ticket: Ticket;
		/** false = 静态展示（详情页顶卡）；默认整卡链接到详情 */
		link?: boolean;
	}

	let { ticket, link = true }: Props = $props();

	const href = $derived(link ? `/tickets/${ticket.id}` : undefined);
</script>

{#if isKind(ticket, 'train')}
	<TransitCard {ticket} {href} />
{:else if isKind(ticket, 'flight')}
	<TransitCard {ticket} {href} />
{:else}
	<StubCard {ticket} {href} />
{/if}
