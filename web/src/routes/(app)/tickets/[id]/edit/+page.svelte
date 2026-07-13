<script lang="ts">
	/**
	 * 编辑票根：加载后回填 TicketForm（edit 模式）→ outbox.updateTicket。
	 * amountCents 等交易字段变更由服务端同步改关联交易（PROTOCOL §5）。
	 */
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { api, ApiError } from '$lib/api/client';
	import { ERR, type Ticket, type TicketInput } from '$lib/api/types';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import TicketForm from '$lib/components/ticket/TicketForm.svelte';
	import { outbox } from '$lib/db/outbox';

	let ticket = $state<Ticket | null>(null);
	let loading = $state(true);
	let notFound = $state(false);
	let submitting = $state(false);
	let submitError = $state('');

	$effect(() => {
		const id = page.params.id ?? '';
		let alive = true;
		loading = true;
		notFound = false;
		ticket = null;

		api
			.getTicket(id)
			.then((t) => {
				if (!alive) return;
				ticket = t;
				loading = false;
			})
			.catch(() => {
				if (!alive) return;
				notFound = true;
				loading = false;
			});

		return () => {
			alive = false;
		};
	});

	async function update(patch: Partial<TicketInput>) {
		if (!ticket || submitting) return;
		submitting = true;
		submitError = '';
		try {
			const updated = await outbox.updateTicket(ticket.id, patch);
			await goto(`/tickets/${updated.id}`, { replaceState: true });
		} catch (error) {
			if (error instanceof ApiError && error.code === ERR.NOT_FOUND) {
				submitError = '票根不存在或已被删除';
			} else {
				submitError = error instanceof ApiError ? error.message : '保存失败，请稍后重试';
			}
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>编辑票根 · 拾光票局</title>
</svelte:head>

<header class="form-head">
	<a
		class="back"
		href={ticket ? `/tickets/${ticket.id}` : '/tickets'}
		aria-label="返回详情"
	>
		← 返回
	</a>
	<h1>编辑票根</h1>
</header>

{#if loading}
	<div class="skeleton-card" aria-hidden="true">
		<Skeleton lines={5} />
	</div>
{:else if notFound}
	<EmptyState
		emoji="🕳️"
		title="票根不存在"
		description="它可能已被删除，或链接有误"
		actionLabel="回到票夹"
		onaction={() => void goto('/tickets')}
	/>
{:else if ticket}
	<TicketForm
		initial={ticket}
		{submitting}
		error={submitError}
		onupdate={(patch) => void update(patch)}
	/>
{/if}

<style>
	.form-head {
		display: flex;
		align-items: center;
		gap: 12px;
		padding-block: 16px;
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

	h1 {
		margin: 0;
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 700;
		color: var(--ink);
	}

	.skeleton-card {
		padding: 16px;
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
	}
</style>
