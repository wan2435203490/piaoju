<script lang="ts">
	/**
	 * 票详情页：票根大卡 + 票面信息 + 内嵌交易摘要 + 照片 + 评价，
	 * 编辑入口与二次确认删除（5 秒可撤销，见 pending-delete）。
	 */
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api/client';
	import { ERR, type Category, type Ticket } from '$lib/api/types';
	import Amount from '$lib/components/Amount.svelte';
	import Button from '$lib/components/Button.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Sheet from '$lib/components/Sheet.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import Rating from '$lib/components/ticket/Rating.svelte';
	import TicketCard from '$lib/components/ticket/TicketCard.svelte';
	import {
		KIND_META,
		PAYMENT_LABEL,
		extraRows,
		fmtDateTime
	} from '$lib/components/ticket/kinds';
	import { pendingDelete } from '$lib/components/ticket/pending-delete.svelte';

	let ticket = $state<Ticket | null>(null);
	let categories = $state<Category[]>([]);
	let loading = $state(true);
	let notFound = $state(false);
	let loadError = $state('');
	let confirmOpen = $state(false);

	$effect(() => {
		const id = page.params.id ?? '';
		let alive = true;
		loading = true;
		notFound = false;
		loadError = '';
		ticket = null;

		api
			.getTicket(id)
			.then((t) => {
				if (!alive) return;
				ticket = t;
				loading = false;
			})
			.catch((error: unknown) => {
				if (!alive) return;
				if (error instanceof ApiError && error.code === ERR.NOT_FOUND) {
					notFound = true;
				} else {
					loadError = error instanceof ApiError ? error.message : '加载失败，请稍后重试';
				}
				loading = false;
			});

		// 分类名非致命：失败只影响分类展示
		api
			.listCategories()
			.then((items) => {
				if (alive) categories = items;
			})
			.catch(() => {});

		return () => {
			alive = false;
		};
	});

	const category = $derived(
		ticket ? categories.find((c) => c.id === ticket!.transaction.categoryId) : undefined
	);
	const rows = $derived(ticket ? extraRows(ticket) : []);

	function confirmDelete() {
		if (!ticket) return;
		confirmOpen = false;
		// 5 秒撤销窗口在票夹列表页呈现（design §4）
		pendingDelete.schedule(ticket);
		void goto('/tickets');
	}
</script>

<svelte:head>
	<title>{ticket ? `${ticket.title} · 票夹` : '票根'} · 拾光票局</title>
</svelte:head>

<header class="detail-head">
	<a class="back" href="/tickets" aria-label="返回票夹">← 票夹</a>
	{#if ticket}
		<span class="kind-tag" style:--tag-color={KIND_META[ticket.kind].color}>
			<span aria-hidden="true">{KIND_META[ticket.kind].emoji}</span>
			{KIND_META[ticket.kind].label}
		</span>
	{/if}
</header>

{#if loading}
	<div class="skeleton-card" aria-hidden="true">
		<Skeleton lines={4} />
	</div>
{:else if notFound}
	<EmptyState
		emoji="🕳️"
		title="票根不存在"
		description="它可能已被删除，或链接有误"
		actionLabel="回到票夹"
		onaction={() => void goto('/tickets')}
	/>
{:else if loadError}
	<EmptyState
		emoji="🌫️"
		title="加载失败"
		description={loadError}
		actionLabel="回到票夹"
		onaction={() => void goto('/tickets')}
	/>
{:else if ticket}
	<div class="detail">
		<!-- 票根大卡（静态，不再链接自身） -->
		<TicketCard {ticket} link={false} />

		<!-- 票面信息 -->
		<section class="card" aria-label="票面信息">
			<h2 class="card-title">票面信息</h2>
			<dl class="rows">
				{#if ticket.venue}
					<div class="row">
						<dt>场馆</dt>
						<dd>{ticket.venue}</dd>
					</div>
				{/if}
				<div class="row">
					<dt>时间</dt>
					<dd class="tnum">{fmtDateTime(ticket.eventTime)}</dd>
				</div>
				{#if ticket.seat}
					<div class="row">
						<dt>座位</dt>
						<dd>{ticket.seat}</dd>
					</div>
				{/if}
				{#each rows as row (row.label)}
					<div class="row">
						<dt>{row.label}</dt>
						<dd class="tnum">{row.value}</dd>
					</div>
				{/each}
			</dl>
		</section>

		<!-- 内嵌交易摘要（只读，PROTOCOL §5） -->
		<section class="card" aria-label="关联交易">
			<h2 class="card-title">关联交易</h2>
			<div class="tx">
				<Amount cents={ticket.transaction.amountCents} direction="expense" size="lg" />
				<div class="tx-meta">
					{#if category}
						<span class="tx-chip">
							<span aria-hidden="true">{category.icon}</span>
							{category.name}
						</span>
					{/if}
					<span class="tx-chip">{PAYMENT_LABEL[ticket.transaction.paymentMethod]}</span>
				</div>
				<p class="tx-hint">删除票根会一并删除这笔交易</p>
			</div>
		</section>

		<!-- 照片 -->
		{#if ticket.attachments.length > 0}
			<section class="card" aria-label="票面照片">
				<h2 class="card-title">照片</h2>
				<div class="photos">
					{#each ticket.attachments as attachment (attachment.id)}
						<a class="photo" href={attachment.url} target="_blank" rel="noreferrer">
							<img src={attachment.thumbUrl} alt="{ticket.title} 票面照片" loading="lazy" />
						</a>
					{/each}
				</div>
			</section>
		{/if}

		<!-- 评价 -->
		<section class="card" aria-label="评价">
			<h2 class="card-title">评价</h2>
			{#if ticket.rating > 0}
				<Rating value={ticket.rating} size="md" />
			{:else}
				<p class="muted">未评分</p>
			{/if}
			{#if ticket.memo}
				<p class="memo">{ticket.memo}</p>
			{/if}
		</section>

		<div class="actions">
			<Button variant="ghost" block onclick={() => void goto(`/tickets/${ticket!.id}/edit`)}>
				编辑
			</Button>
			<Button variant="danger" block onclick={() => (confirmOpen = true)}>删除</Button>
		</div>

		<p class="stamp tnum">
			存入于 {fmtDateTime(ticket.createdAt)}
			{#if ticket.updatedAt !== ticket.createdAt}
				· 更新于 {fmtDateTime(ticket.updatedAt)}
			{/if}
		</p>
	</div>

	<!-- 删除二次确认（design §4） -->
	<Sheet bind:open={confirmOpen} title="删除这张票根？">
		<div class="confirm">
			<p class="confirm-text">「{ticket.title}」与关联的一笔交易将一起删除，删除后 5 秒内可撤销。</p>
			<Button variant="danger" block onclick={confirmDelete}>删除票根</Button>
			<Button variant="ghost" block onclick={() => (confirmOpen = false)}>再想想</Button>
		</div>
	</Sheet>
{/if}

<style>
	.detail-head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
		padding-block: 16px 12px;
	}

	.back {
		display: inline-flex;
		align-items: center;
		min-height: 44px; /* 触控目标 */
		color: var(--ink-2);
		font-size: 0.875rem; /* 14 正文 */
		text-decoration: none;
	}

	.back:active {
		color: var(--brand);
	}

	.kind-tag {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 4px 8px;
		border-radius: var(--radius-chip);
		background: color-mix(in srgb, var(--tag-color) 14%, transparent);
		color: var(--ink);
		font-size: 0.75rem; /* 12 辅助 */
		font-weight: 600;
	}

	.detail {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.skeleton-card {
		padding: 16px;
		background: var(--surface);
		border-radius: var(--radius-ticket);
		box-shadow: var(--shadow-card);
	}

	.card {
		padding: 16px;
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
	}

	.card-title {
		margin: 0 0 12px;
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 600;
		color: var(--ink-2);
	}

	/* ---- 票面信息行 ---- */
	.rows {
		display: flex;
		flex-direction: column;
		gap: 0;
		margin: 0;
	}

	.row {
		display: flex;
		justify-content: space-between;
		gap: 16px;
		padding-block: 8px;
	}

	.row + .row {
		border-top: 1px solid var(--line);
	}

	dt {
		flex: none;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
	}

	dd {
		margin: 0;
		font-size: 0.875rem;
		color: var(--ink);
		text-align: right;
		overflow-wrap: anywhere;
	}

	/* ---- 交易摘要 ---- */
	.tx {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.tx-meta {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}

	.tx-chip {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		padding: 4px 8px;
		border-radius: var(--radius-chip);
		background: var(--bg);
		color: var(--ink);
		font-size: 0.75rem; /* 12 辅助 */
	}

	.tx-hint {
		margin: 0;
		font-size: 0.75rem;
		color: var(--ink-2);
	}

	/* ---- 照片 ---- */
	.photos {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 8px;
	}

	.photo {
		display: block;
		aspect-ratio: 1;
		border-radius: var(--radius-card);
		overflow: hidden;
	}

	.photo img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}

	/* ---- 评价 ---- */
	.muted {
		margin: 0;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
	}

	.memo {
		margin: 8px 0 0;
		font-size: 0.875rem;
		color: var(--ink);
		line-height: 1.5;
		white-space: pre-wrap;
	}

	/* ---- 操作 ---- */
	.actions {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}

	.stamp {
		margin: 0;
		text-align: center;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.confirm {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding-block: 4px 8px;
	}

	.confirm-text {
		margin: 0;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
		text-align: center;
	}
</style>
