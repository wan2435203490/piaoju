<script lang="ts">
	/**
	 * 五套票型表单（create/edit 共用，PROTOCOL §5 TicketInput 逐字段对齐）：
	 * - kind 切换（仅新建）→ 专属 extra 字段组随之切换（extra 字段表见 kinds.ts）
	 * - 共享交易段：金额（元 → 整数分）/ 分类 / 支付方式 / 记账时间
	 * - 照片：outbox.upload 占位上传（mock 返回内联占位图），attachmentIds 随单提交
	 * - 新建 id 在挂载时生成一次（客户端 UUIDv4，重试幂等 upsert 不产生重复单）
	 * - 编辑不提交 occurredAt（Ticket 内嵌交易摘要不含该字段，避免盲改）
	 */
	import { api } from '$lib/api/client';
	import type {
		Attachment,
		Category,
		PaymentMethod,
		Ticket,
		TicketExtra,
		TicketInput,
		TicketKind
	} from '$lib/api/types';
	import { PAYMENT_METHODS, TICKET_KINDS } from '$lib/api/types';
	import Button from '$lib/components/Button.svelte';
	import CategoryPicker from '$lib/components/CategoryPicker.svelte';
	import { outbox } from '$lib/db/outbox';
	import { uuid } from '$lib/utils/uuid';
	import Field from './Field.svelte';
	import Rating from './Rating.svelte';
	import {
		EXTRA_FIELDS,
		KIND_META,
		PAYMENT_LABEL,
		centsToYuanInput,
		isoToLocalInput,
		localInputToIso,
		nowLocalInput,
		parseYuanToCents
	} from './kinds';

	interface Props {
		/** 传入 = 编辑模式（kind 锁定、不提交 occurredAt） */
		initial?: Ticket;
		submitting?: boolean;
		/** 页面层的提交错误（信封 message 等），显示在提交按钮上方 */
		error?: string;
		/** 新建提交（完整 TicketInput） */
		oncreate?: (input: TicketInput) => void;
		/** 编辑提交（不含 id/occurredAt 的 patch） */
		onupdate?: (patch: Partial<TicketInput>) => void;
	}

	let { initial, submitting = false, error = '', oncreate, onupdate }: Props = $props();

	const isEdit = initial !== undefined;
	/** 新建票的业务主键：挂载时生成一次，提交重试不变（conventions §1 幂等） */
	const createId = uuid();

	/* ---------- 表单状态（编辑模式回填 initial） ---------- */

	function blankExtra(kind: TicketKind): Record<string, string> {
		return Object.fromEntries(EXTRA_FIELDS[kind].map((def) => [def.key, '']));
	}

	function initExtra(ticket: Ticket): Record<string, string> {
		const src = ticket.extra as Record<string, string>;
		return Object.fromEntries(
			EXTRA_FIELDS[ticket.kind].map((def) => [
				def.key,
				def.type === 'datetime' ? isoToLocalInput(src[def.key] ?? '') : (src[def.key] ?? '')
			])
		);
	}

	let kind = $state<TicketKind>(initial?.kind ?? 'movie');
	let title = $state(initial?.title ?? '');
	let venue = $state(initial?.venue ?? '');
	let seat = $state(initial?.seat ?? '');
	/* 日期默认今天、时间默认现在（design §4） */
	let eventTimeLocal = $state(initial ? isoToLocalInput(initial.eventTime) : nowLocalInput());
	let extra = $state<Record<string, string>>(initial ? initExtra(initial) : blankExtra('movie'));
	let rating = $state(initial?.rating ?? 0);
	let memo = $state(initial?.memo ?? '');

	let amountYuan = $state(initial ? centsToYuanInput(initial.transaction.amountCents) : '');
	let categoryId = $state<number | null>(initial?.transaction.categoryId ?? null);
	let paymentMethod = $state<PaymentMethod>(initial?.transaction.paymentMethod ?? 'wechat');
	let occurredAtLocal = $state(nowLocalInput());

	let attachments = $state<Attachment[]>(initial ? [...initial.attachments] : []);

	function switchKind(next: TicketKind) {
		if (kind === next) return;
		kind = next;
		extra = blankExtra(next); // 换票型即换 extra 形状，旧值不跨型残留
	}

	/* ---------- 分类（读操作直接走 api；只展示支出分类） ---------- */

	let categories = $state<Category[]>([]);
	let categoryError = $state('');

	$effect(() => {
		let alive = true;
		api
			.listCategories()
			.then((items) => {
				if (alive) categories = items.filter((c) => c.kind === 'expense');
			})
			.catch(() => {
				if (alive) categoryError = '分类加载失败，请稍后重试';
			});
		return () => {
			alive = false;
		};
	});

	/* ---------- 照片上传（outbox.upload，mock 返回占位图） ---------- */

	let fileInput = $state<HTMLInputElement | null>(null);
	let uploading = $state(false);
	let uploadError = $state('');

	async function onFilesPicked(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const files = [...(input.files ?? [])];
		input.value = '';
		if (files.length === 0) return;
		uploading = true;
		uploadError = '';
		try {
			for (const file of files) {
				attachments.push(await outbox.upload(file));
			}
		} catch {
			uploadError = '照片上传失败，请重试';
		} finally {
			uploading = false;
		}
	}

	function removeAttachment(id: number) {
		attachments = attachments.filter((a) => a.id !== id);
	}

	/* ---------- 校验 + 提交 ---------- */

	let validationError = $state('');

	function buildExtra(): TicketExtra {
		return Object.fromEntries(
			EXTRA_FIELDS[kind].map((def) => [
				def.key,
				def.type === 'datetime' ? localInputToIso(extra[def.key] ?? '') : (extra[def.key] ?? '').trim()
			])
		) as unknown as TicketExtra;
	}

	function submit(event: SubmitEvent) {
		event.preventDefault();
		const cents = parseYuanToCents(amountYuan);
		if (!title.trim()) {
			validationError = '请填写标题';
		} else if (!eventTimeLocal) {
			validationError = '请选择时间';
		} else if (cents === null || cents <= 0) {
			validationError = '请输入正确的金额（大于 0）';
		} else if (categoryId === null) {
			validationError = '请选择分类';
		} else {
			validationError = '';
			const shared = {
				kind,
				title: title.trim(),
				venue: venue.trim(),
				eventTime: localInputToIso(eventTimeLocal),
				seat: seat.trim(),
				extra: buildExtra(),
				rating,
				memo: memo.trim(),
				amountCents: cents,
				categoryId,
				paymentMethod,
				attachmentIds: attachments.map((a) => a.id)
			};
			if (isEdit) {
				onupdate?.(shared);
			} else {
				oncreate?.({ ...shared, id: createId, occurredAt: localInputToIso(occurredAtLocal) });
			}
		}
	}

	const shownError = $derived(validationError || error);
</script>

<form class="form" onsubmit={submit} novalidate>
	<!-- 票型（新建可选；编辑锁定） -->
	<section class="section" aria-label="票型">
		{#if isEdit}
			<div class="kind-fixed">
				<span aria-hidden="true">{KIND_META[kind].emoji}</span>
				<span>{KIND_META[kind].label}</span>
				<span class="kind-hint">票型创建后不可更改</span>
			</div>
		{:else}
			<div class="kinds" role="radiogroup" aria-label="选择票型">
				{#each TICKET_KINDS as k (k)}
					<button
						type="button"
						class="kind-chip"
						class:selected={kind === k}
						style:--chip-color={KIND_META[k].color}
						role="radio"
						aria-checked={kind === k}
						onclick={() => switchKind(k)}
					>
						<span aria-hidden="true">{KIND_META[k].emoji}</span>
						{KIND_META[k].label}
					</button>
				{/each}
			</div>
		{/if}
	</section>

	<!-- 基本信息 -->
	<section class="section" aria-label="基本信息">
		<Field id="tk-title" label="标题" bind:value={title} placeholder="片名 / 演出 / 行程…" />
		<Field id="tk-venue" label="场馆 / 地点" bind:value={venue} placeholder="影院、剧场、车站…" />
		<div class="pair">
			<Field id="tk-time" label="时间" type="datetime-local" bind:value={eventTimeLocal} />
			<Field id="tk-seat" label="座位" bind:value={seat} placeholder="9排12座" />
		</div>
	</section>

	<!-- 票型专属字段（PROTOCOL §5 extra 表） -->
	{#if EXTRA_FIELDS[kind].length > 0}
		<section class="section" aria-label="{KIND_META[kind].label}信息">
			<h2 class="section-title">{KIND_META[kind].label}信息</h2>
			{#each EXTRA_FIELDS[kind] as def (kind + def.key)}
				<Field
					id="tk-extra-{def.key}"
					label={def.label}
					type={def.type === 'datetime' ? 'datetime-local' : 'text'}
					bind:value={extra[def.key]}
					placeholder={def.placeholder ?? ''}
				/>
			{/each}
		</section>
	{/if}

	<!-- 照片（票面存档） -->
	<section class="section" aria-label="票面照片">
		<h2 class="section-title">票面照片</h2>
		<div class="photos">
			{#each attachments as attachment (attachment.id)}
				<div class="photo-cell">
					<img src={attachment.thumbUrl} alt="票面照片" loading="lazy" />
					<button
						type="button"
						class="photo-remove"
						aria-label="移除照片"
						onclick={() => removeAttachment(attachment.id)}
					>
						✕
					</button>
				</div>
			{/each}
			<button
				type="button"
				class="photo-add"
				disabled={uploading}
				onclick={() => fileInput?.click()}
			>
				{uploading ? '上传中…' : '＋ 添加'}
			</button>
		</div>
		{#if uploadError}
			<p class="field-error" role="alert">{uploadError}</p>
		{/if}
		<input
			bind:this={fileInput}
			type="file"
			accept="image/jpeg,image/png,image/webp"
			multiple
			class="file-hidden"
			onchange={onFilesPicked}
			aria-hidden="true"
			tabindex="-1"
		/>
	</section>

	<!-- 评价 -->
	<section class="section" aria-label="评价">
		<h2 class="section-title">评价</h2>
		<Rating value={rating} size="md" onchange={(v) => (rating = v)} />
		<Field id="tk-memo" label="感想" type="textarea" bind:value={memo} placeholder="值得记一笔的瞬间…" />
	</section>

	<!-- 共享交易段：金额 / 分类 / 支付方式 / 记账时间 -->
	<section class="section" aria-label="记一笔账">
		<h2 class="section-title">记一笔账</h2>
		<Field
			id="tk-amount"
			label="金额（元）"
			bind:value={amountYuan}
			placeholder="0.00"
			inputmode="decimal"
		/>
		<div class="sub-block">
			<span class="sub-label" id="tk-category-label">分类</span>
			{#if categoryError}
				<p class="field-error" role="alert">{categoryError}</p>
			{:else}
				<CategoryPicker
					{categories}
					selected={categoryId}
					onselect={(category) => (categoryId = category.id)}
				/>
			{/if}
		</div>
		<div class="sub-block">
			<span class="sub-label" id="tk-pay-label">支付方式</span>
			<div class="pays" role="radiogroup" aria-labelledby="tk-pay-label">
				{#each PAYMENT_METHODS as method (method)}
					<button
						type="button"
						class="pay-chip"
						class:selected={paymentMethod === method}
						role="radio"
						aria-checked={paymentMethod === method}
						onclick={() => (paymentMethod = method)}
					>
						{PAYMENT_LABEL[method]}
					</button>
				{/each}
			</div>
		</div>
		{#if !isEdit}
			<Field id="tk-occurred" label="记账时间" type="datetime-local" bind:value={occurredAtLocal} />
		{/if}
	</section>

	{#if shownError}
		<div class="form-error" role="alert">{shownError}</div>
	{/if}

	<Button type="submit" block loading={submitting}>
		{#if submitting}保存中…{:else}{isEdit ? '保存修改' : '存入票夹'}{/if}
	</Button>
</form>

<style>
	.form {
		display: flex;
		flex-direction: column;
		gap: 24px;
	}

	.section {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.section-title {
		margin: 0;
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 600;
		color: var(--ink-2);
	}

	/* ---- 票型选择 ---- */
	.kinds {
		display: flex;
		gap: 8px;
		overflow-x: auto;
		padding-bottom: 4px;
		scrollbar-width: none;
	}

	.kind-chip {
		flex: none;
		display: inline-flex;
		align-items: center;
		gap: 4px;
		min-height: 44px; /* 触控目标 */
		padding: 0 12px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--surface);
		color: var(--ink-2);
		font-family: inherit;
		font-size: 0.875rem; /* 14 正文 */
		cursor: pointer;
		transition:
			border-color var(--dur-fast) var(--ease),
			background-color var(--dur-fast) var(--ease),
			color var(--dur-fast) var(--ease);
	}

	.kind-chip.selected {
		border-color: var(--chip-color);
		background: color-mix(in srgb, var(--chip-color) 12%, transparent);
		color: var(--ink);
		font-weight: 600;
	}

	.kind-fixed {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 44px;
		padding: 0 12px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--bg);
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--ink);
	}

	.kind-hint {
		margin-left: auto;
		font-size: 0.75rem; /* 12 辅助 */
		font-weight: 400;
		color: var(--ink-2);
	}

	.pair {
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 12px;
	}

	/* ---- 照片 ---- */
	.photos {
		display: grid;
		grid-template-columns: repeat(3, 1fr);
		gap: 8px;
	}

	.photo-cell {
		position: relative;
		aspect-ratio: 1;
		border-radius: var(--radius-card);
		overflow: hidden;
	}

	.photo-cell img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}

	.photo-remove {
		position: absolute;
		top: 4px;
		right: 4px;
		display: grid;
		place-items: center;
		width: 28px;
		height: 28px;
		padding: 0;
		border: none;
		border-radius: 50%;
		background: var(--scrim);
		color: var(--surface);
		font-size: 0.75rem;
		cursor: pointer;
	}

	.photo-add {
		aspect-ratio: 1;
		border: 1px dashed var(--line);
		border-radius: var(--radius-card);
		background: transparent;
		color: var(--ink-2);
		font-family: inherit;
		font-size: 0.875rem; /* 14 正文 */
		cursor: pointer;
		transition: border-color var(--dur-fast) var(--ease);
	}

	.photo-add:active:not(:disabled) {
		border-color: var(--brand);
		color: var(--brand);
	}

	.photo-add:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.file-hidden {
		display: none;
	}

	/* ---- 分类 / 支付方式 ---- */
	.sub-block {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.sub-label {
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 500;
		color: var(--ink);
	}

	.pays {
		display: flex;
		flex-wrap: wrap;
		gap: 8px;
	}

	.pay-chip {
		min-height: 44px; /* 触控目标 */
		padding: 0 12px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--surface);
		color: var(--ink-2);
		font-family: inherit;
		font-size: 0.875rem;
		cursor: pointer;
		transition:
			border-color var(--dur-fast) var(--ease),
			background-color var(--dur-fast) var(--ease),
			color var(--dur-fast) var(--ease);
	}

	.pay-chip.selected {
		border-color: var(--brand);
		background: color-mix(in srgb, var(--brand) 10%, transparent);
		color: var(--brand);
		font-weight: 600;
	}

	/* ---- 错误 ---- */
	.field-error {
		margin: 0;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--danger);
	}

	.form-error {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--danger);
		background: color-mix(in srgb, var(--danger) 8%, transparent);
		border: 1px solid color-mix(in srgb, var(--danger) 40%, transparent);
		border-radius: var(--radius-btn);
		padding: 12px;
	}
</style>
