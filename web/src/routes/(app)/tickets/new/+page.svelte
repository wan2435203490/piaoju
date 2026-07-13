<script lang="ts">
	/**
	 * 新建票根：TicketForm（create 模式）→ outbox.createTicket（M3 换离线队列无感知）。
	 * id 由表单在挂载时生成（客户端 UUIDv4），服务端幂等 upsert。
	 */
	import { goto } from '$app/navigation';
	import { ApiError } from '$lib/api/client';
	import type { TicketInput } from '$lib/api/types';
	import TicketForm from '$lib/components/ticket/TicketForm.svelte';
	import { outbox } from '$lib/db/outbox';

	let submitting = $state(false);
	let submitError = $state('');

	async function create(input: TicketInput) {
		if (submitting) return;
		submitting = true;
		submitError = '';
		try {
			const ticket = await outbox.createTicket(input);
			// replaceState：返回时跳过表单页，直接回到票夹
			await goto(`/tickets/${ticket.id}`, { replaceState: true });
		} catch (error) {
			submitError = error instanceof ApiError ? error.message : '保存失败，请稍后重试';
		} finally {
			submitting = false;
		}
	}
</script>

<svelte:head>
	<title>存一张票根 · 拾光票局</title>
</svelte:head>

<header class="form-head">
	<a class="back" href="/tickets" aria-label="返回票夹">← 票夹</a>
	<h1>存一张票根</h1>
</header>

<TicketForm {submitting} error={submitError} oncreate={(input) => void create(input)} />

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
</style>
