<script module lang="ts">
	/**
	 * 识票服务未配置（50001）→ 整个会话内不再渲染「拍照识别」入口。
	 * 模块级：换页/重开表单也不复现，避免反复让用户点一个注定失败的按钮（W6 任务卡）。
	 */
	let recognizeOffForSession = false;
</script>

<script lang="ts">
	/**
	 * 五套票型表单（create/edit 共用，PROTOCOL §5 TicketInput 逐字段对齐）：
	 * - kind 切换（仅新建）→ 专属 extra 字段组随之切换（extra 字段表见 kinds.ts）
	 * - 共享交易段：金额（元 → 整数分）/ 分类 / 支付方式 / 记账时间
	 * - 照片：outbox.upload 占位上传（mock 返回内联占位图），attachmentIds 随单提交
	 * - 新建 id 在挂载时生成一次（客户端 UUIDv4，重试幂等 upsert 不产生重复单）
	 * - 编辑不提交 occurredAt（Ticket 内嵌交易摘要不含该字段，避免盲改）
	 * - 识票（W6，PROTOCOL §6.1）：仅新建模式。上传票面照 → recognize → **回填草稿**，
	 *   永不自动提交；离线（附件为负数临时 id）不可识别；50001 后本会话隐藏入口。
	 */
	import { untrack } from 'svelte';
	import { ApiError, api } from '$lib/api/client';
	import type {
		Attachment,
		Category,
		PaymentMethod,
		Ticket,
		TicketDraft,
		TicketExtra,
		TicketInput,
		TicketKind
	} from '$lib/api/types';
	import { ERR, PAYMENT_METHODS, RECOGNIZE_CONFIDENCE_FLOOR, TICKET_KINDS } from '$lib/api/types';
	import Button from '$lib/components/Button.svelte';
	import CategoryPicker from '$lib/components/CategoryPicker.svelte';
	import { outbox } from '$lib/db/outbox';
	import { capturePhoto } from '$lib/native/camera';
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

	/** 表单以本地草稿为准：initial 刻意只取一次做种子（untrack 声明该意图） */
	const init = untrack(() => initial);
	const isEdit = init !== undefined;
	/** 新建票的业务主键：挂载时生成一次，提交重试不变（conventions §1 幂等） */
	// 票根与其联动交易的主键都由客户端生成（契约 §5 v1.2）：离线建票时本地要同时
	// 写入 tickets 和 transactions，交易 id 等不到服务端。组件实例内固定，重试不换 id。
	const createId = uuid();
	const createTransactionId = uuid();

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

	let kind = $state<TicketKind>(init?.kind ?? 'movie');
	let title = $state(init?.title ?? '');
	let venue = $state(init?.venue ?? '');
	let seat = $state(init?.seat ?? '');
	/* 日期默认今天、时间默认现在（design §4） */
	let eventTimeLocal = $state(init ? isoToLocalInput(init.eventTime) : nowLocalInput());
	let extra = $state<Record<string, string>>(init ? initExtra(init) : blankExtra('movie'));
	let rating = $state(init?.rating ?? 0);
	let memo = $state(init?.memo ?? '');

	let amountYuan = $state(init ? centsToYuanInput(init.transaction.amountCents) : '');
	let categoryId = $state<number | null>(init?.transaction.categoryId ?? null);
	let paymentMethod = $state<PaymentMethod>(init?.transaction.paymentMethod ?? 'wechat');
	let occurredAtLocal = $state(nowLocalInput());

	let attachments = $state<Attachment[]>(init ? [...init.attachments] : []);

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

	/** 上传一批照片进附件（原生相机与文件选择器共用） */
	async function addPhotoFiles(files: File[]) {
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

	function onFilesPicked(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const files = [...(input.files ?? [])];
		input.value = '';
		void addPhotoFiles(files);
	}

	/** 加照片按钮：原生 App 先唤起系统相机；Web 或取消则回退文件选择器 */
	async function onAddPhotoClick() {
		const photo = await capturePhoto();
		if (photo) void addPhotoFiles([photo]);
		else fileInput?.click();
	}

	function removeAttachment(id: number) {
		attachments = attachments.filter((a) => a.id !== id);
	}

	/* ---------- 识票（PROTOCOL §6.1；仅新建模式） ---------- */

	let recognizeInput = $state<HTMLInputElement | null>(null);
	let recognizing = $state(false);
	/** 50001：识票服务未开启 → 本会话隐藏入口 */
	let recognizeOff = $state(recognizeOffForSession);
	/** 识别失败提示：不打断，用户照常手填 */
	let recognizeError = $state('');
	/** confidence < 0.6 → 顶部提醒核对 */
	let lowConfidence = $state(false);
	/** 本次识别回填了哪些字段（'title' / 'amount' / 'extra:hall' …），用于视觉反馈 */
	let filledKeys = $state<string[]>([]);
	/** 离线时 outbox.upload 返回负数临时 id，识票不可用 */
	let online = $state(true);

	$effect(() => {
		online = navigator.onLine;
		const on = () => (online = true);
		const off = () => (online = false);
		window.addEventListener('online', on);
		window.addEventListener('offline', off);
		return () => {
			window.removeEventListener('online', on);
			window.removeEventListener('offline', off);
		};
	});

	const isFilled = (key: string) => filledKeys.includes(key);
	/** 识别入口可见性：编辑模式不给（票型锁定）、服务未开启后永久隐藏 */
	const showRecognize = $derived(!isEdit && !recognizeOff);

	/** 草稿 → 表单（只回填非零值，永不自动提交；契约 §6.1 识别不出的字段回零值） */
	function applyDraft(draft: TicketDraft) {
		const keys: string[] = [];
		if (draft.kind && draft.kind !== kind) switchKind(draft.kind); // 换型会重置 extra，须先做
		if (draft.title) {
			title = draft.title;
			keys.push('title');
		}
		if (draft.venue) {
			venue = draft.venue;
			keys.push('venue');
		}
		const eventLocal = isoToLocalInput(draft.eventTime);
		if (eventLocal) {
			eventTimeLocal = eventLocal;
			keys.push('eventTime');
		}
		if (draft.seat) {
			seat = draft.seat;
			keys.push('seat');
		}
		if (draft.amountCents > 0) {
			amountYuan = centsToYuanInput(draft.amountCents);
			keys.push('amount');
		}
		// extra 只取当前票型白名单内的键（契约 §5 extra 表），未知键丢弃
		const src = (draft.extra ?? {}) as Record<string, string>;
		const next = { ...extra };
		for (const def of EXTRA_FIELDS[draft.kind ?? kind]) {
			const raw = src[def.key];
			if (!raw) continue;
			// datetime 型：RFC3339 → 本地输入值；无法解析回 '' 时不回填也不高亮（避免「已识别」空字段）
			const value = def.type === 'datetime' ? isoToLocalInput(raw) : raw;
			if (!value) continue;
			next[def.key] = value;
			keys.push(`extra:${def.key}`);
		}
		extra = next;
		filledKeys = keys;
		lowConfidence = draft.confidence < RECOGNIZE_CONFIDENCE_FLOOR;
	}

	/** 识票主流程（原生相机与文件选择器共用） */
	async function recognizeWithFile(file: File) {
		if (recognizing) return;
		recognizing = true;
		recognizeError = '';
		lowConfidence = false;
		filledKeys = [];
		try {
			// 1）先上传（票面照同时存进本票的附件里，不白拍）
			const attachment = await outbox.upload(file);
			attachments.push(attachment);
			// 2）离线：附件是本地负数临时 id，服务端无从识别 → 提示联网后再试
			if (attachment.id < 0) {
				recognizeError = '照片已保存，联网后可识别';
				return;
			}
			// 3）识别 → 回填草稿（用户可改，必须手动提交）
			applyDraft(await api.recognizeTicket(attachment.id));
		} catch (error) {
			if (error instanceof ApiError && error.code === ERR.RECOGNIZE_UNAVAILABLE) {
				// 50001：功能与主流程解耦 —— 本会话不再显示入口
				recognizeOffForSession = true;
				recognizeOff = true;
				recognizeError = '识票服务未开启，请手动填写';
			} else if (error instanceof ApiError && error.code === ERR.RECOGNIZE_RATE_LIMITED) {
				recognizeError = '识别次数已达上限，请稍后重试';
			} else {
				// 其他错误不打断：用户照常手填
				recognizeError = '识别失败，可手动填写';
			}
		} finally {
			recognizing = false;
		}
	}

	function onRecognizePicked(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (file) void recognizeWithFile(file);
	}

	/** 拍照识别按钮：原生 App 先唤起系统相机；Web 或取消则回退文件选择器 */
	async function onRecognizeClick() {
		const photo = await capturePhoto();
		if (photo) void recognizeWithFile(photo);
		else recognizeInput?.click();
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
				oncreate?.({
					...shared,
					id: createId,
					transactionId: createTransactionId,
					occurredAt: localInputToIso(occurredAtLocal)
				});
			}
		}
	}

	const shownError = $derived(validationError || error);
</script>

<!-- 识别回填标记（视觉反馈：哪些字段是机器填的） -->
{#snippet recBadge(shown: boolean)}
	{#if shown}
		<span class="rec-badge">识</span>
	{/if}
{/snippet}

<form class="form" onsubmit={submit} novalidate>
	<!-- 识票入口（PROTOCOL §6.1，仅新建；50001 后本会话隐藏） -->
	{#if showRecognize}
		<section class="section rec" aria-label="拍照识别">
			<div class="rec-row">
				<button
					type="button"
					class="rec-btn"
					disabled={recognizing || !online}
					aria-busy={recognizing}
					onclick={onRecognizeClick}
				>
					{#if recognizing}
						<span class="rec-spin" aria-hidden="true"></span>识别中…
					{:else}
						<span aria-hidden="true">📷</span> 拍照识别
					{/if}
				</button>
				<p class="rec-hint">
					{online
						? '拍一张票面，自动填好下面的字段；识别结果可改，不会自动保存'
						: '离线中，联网后可识别（照片仍可先存下来）'}
				</p>
			</div>
			<input
				bind:this={recognizeInput}
				type="file"
				accept="image/jpeg,image/png,image/webp"
				capture="environment"
				class="file-hidden"
				onchange={onRecognizePicked}
				aria-hidden="true"
				tabindex="-1"
			/>
		</section>

		{#if lowConfidence}
			<!-- 契约 §6.1：confidence < 0.6 → 提示核对 -->
			<div class="rec-warn" role="status">
				<span aria-hidden="true">⚠️</span> 识别可能不准，请核对下面的字段
			</div>
		{/if}

		{#if filledKeys.length > 0}
			<div class="rec-ok" role="status">
				已按识别结果填好 {filledKeys.length} 处（带「识」标记的字段），确认无误后再保存
			</div>
		{/if}

		{#if recognizeError}
			<p class="field-error" role="alert">{recognizeError}</p>
		{/if}
	{/if}

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

	<!-- 基本信息（识别回填的字段带「识」标记） -->
	<section class="section" aria-label="基本信息">
		<div class="fw" class:filled={isFilled('title')}>
			<Field id="tk-title" label="标题" bind:value={title} placeholder="片名 / 演出 / 行程…" />
			{@render recBadge(isFilled('title'))}
		</div>
		<div class="fw" class:filled={isFilled('venue')}>
			<Field id="tk-venue" label="场馆 / 地点" bind:value={venue} placeholder="影院、剧场、车站…" />
			{@render recBadge(isFilled('venue'))}
		</div>
		<div class="pair">
			<div class="fw" class:filled={isFilled('eventTime')}>
				<Field id="tk-time" label="时间" type="datetime-local" bind:value={eventTimeLocal} />
				{@render recBadge(isFilled('eventTime'))}
			</div>
			<div class="fw" class:filled={isFilled('seat')}>
				<Field id="tk-seat" label="座位" bind:value={seat} placeholder="9排12座" />
				{@render recBadge(isFilled('seat'))}
			</div>
		</div>
	</section>

	<!-- 票型专属字段（PROTOCOL §5 extra 表） -->
	{#if EXTRA_FIELDS[kind].length > 0}
		<section class="section" aria-label="{KIND_META[kind].label}信息">
			<h2 class="section-title">{KIND_META[kind].label}信息</h2>
			{#each EXTRA_FIELDS[kind] as def (kind + def.key)}
				<div class="fw" class:filled={isFilled(`extra:${def.key}`)}>
					<Field
						id="tk-extra-{def.key}"
						label={def.label}
						type={def.type === 'datetime' ? 'datetime-local' : 'text'}
						bind:value={extra[def.key]}
						placeholder={def.placeholder ?? ''}
					/>
					{@render recBadge(isFilled(`extra:${def.key}`))}
				</div>
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
				onclick={onAddPhotoClick}
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
		<div class="fw" class:filled={isFilled('amount')}>
			<Field
				id="tk-amount"
				label="金额（元）"
				bind:value={amountYuan}
				placeholder="0.00"
				inputmode="decimal"
			/>
			{@render recBadge(isFilled('amount'))}
		</div>
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

	/* ---- 识票入口（§6.1） ---- */
	.rec-row {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
	}

	.rec-btn {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		flex: none;
		min-height: 44px; /* 触控目标 */
		padding: 0 16px;
		border: 1px dashed var(--brand);
		border-radius: var(--radius-btn);
		background: color-mix(in srgb, var(--brand) 8%, transparent);
		color: var(--brand);
		font-family: inherit;
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		cursor: pointer;
		transition:
			transform var(--dur-fast) var(--ease),
			opacity var(--dur-fast) var(--ease);
	}

	.rec-btn:active:not(:disabled) {
		transform: scale(0.97);
	}

	.rec-btn:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.rec-spin {
		width: 16px;
		height: 16px;
		flex: none;
		border: 2px solid currentColor;
		border-top-color: transparent;
		border-radius: 50%;
		animation: spin 0.8s linear infinite; /* prefers-reduced-motion 由 app.css 全局收敛 */
	}

	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}

	.rec-hint {
		flex: 1;
		min-width: 12ch;
		margin: 0;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	/* 提示条：低可信度（暖色警示）/ 回填成功（accent） */
	.rec-warn,
	.rec-ok {
		font-size: 0.875rem; /* 14 正文 */
		border-radius: var(--radius-btn);
		padding: 12px;
	}

	.rec-warn {
		color: var(--ink);
		background: color-mix(in srgb, var(--brand) 14%, transparent);
		border: 1px solid color-mix(in srgb, var(--brand) 45%, transparent);
	}

	.rec-ok {
		color: var(--ink);
		background: color-mix(in srgb, var(--accent) 10%, transparent);
		border: 1px solid color-mix(in srgb, var(--accent) 40%, transparent);
	}

	/* 字段回填标记 */
	.fw {
		position: relative;
	}

	.rec-badge {
		position: absolute;
		top: 0;
		right: 0;
		font-size: 0.75rem; /* 12 辅助 */
		font-weight: 600;
		line-height: 1;
		padding: 2px 6px;
		border-radius: var(--radius-chip);
		background: color-mix(in srgb, var(--accent) 16%, transparent);
		color: var(--accent);
	}

	.fw.filled :global(.input) {
		border-color: color-mix(in srgb, var(--accent) 60%, transparent);
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
		color: var(--on-scrim); /* 遮罩恒为黑，✕ 固定亮色，两种模式对比度都达标 */
		font-size: 0.75rem;
		cursor: pointer;
	}

	/* 视觉保持 28px 圆，触达区扩到 44×44（design §4 触控目标硬规则） */
	.photo-remove::before {
		content: '';
		position: absolute;
		inset: -8px;
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
