<script lang="ts">
	/**
	 * 账单导入（PROTOCOL §6.2，W6）：
	 * 选来源 + 选 CSV → POST /imports/preview（只解析不落库）→ 逐行核对/改分类
	 * → 确认导入。**写入一律走 outbox.createTransaction**（每行一条，id 客户端 UUID）：
	 * 离线安全 + 幂等重放，绝不直接调 api 写。大批量分批 + 进度条，不卡 UI。
	 */
	import { goto } from '$app/navigation';
	import { api } from '$lib/api/client';
	import type { Category, ImportRow, ImportSource } from '$lib/api/types';
	import { IMPORT_MAX_BYTES, IMPORT_SOURCES } from '$lib/api/types';
	import Amount from '$lib/components/Amount.svelte';
	import Button from '$lib/components/Button.svelte';
	import CategoryPicker from '$lib/components/CategoryPicker.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import Sheet from '$lib/components/Sheet.svelte';
	import Skeleton from '$lib/components/Skeleton.svelte';
	import { fmtDateTime } from '$lib/components/ticket/kinds';
	import { data } from '$lib/data';
	import { outbox } from '$lib/db/outbox';
	import { uuid } from '$lib/utils/uuid';
	import {
		IMPORT_BATCH_SIZE,
		SOURCE_EMOJI,
		SOURCE_LABEL,
		categoriesFor,
		categoryLabel,
		chunk,
		defaultSelection,
		previewErrorMessage,
		summarize,
		toTransactionInput,
		toggleRow
	} from './rows';

	type Phase = 'pick' | 'previewing' | 'preview' | 'importing' | 'done';

	let phase = $state<Phase>('pick');
	let source = $state<ImportSource>('wechat');
	let fileName = $state('');
	let fileInput = $state<HTMLInputElement | null>(null);

	let rows = $state<ImportRow[]>([]);
	let selected = $state<number[]>([]);
	/** 用户改过的分类：rowIndex → categoryId */
	let overrides = $state<Record<number, number>>({});
	/** 行的交易主键：预览阶段固定，重试导入复用同一 id → 服务端幂等不写重 */
	let rowIds = $state<Record<number, string>>({});

	let error = $state('');
	let categories = $state<Category[]>([]);
	let categoriesError = $state(false);

	/** 进度（分批提交时刷新） */
	let done = $state(0);
	let result = $state({ ok: 0, failed: 0, skipped: 0 });

	/** 改分类的行（打开 Sheet） */
	let editingRow = $state<ImportRow | null>(null);

	const summary = $derived(summarize(rows, selected));
	const progressPct = $derived(summary.selected === 0 ? 0 : Math.round((done / summary.selected) * 100));

	$effect(() => {
		let alive = true;
		data
			.listCategories()
			.then((items) => {
				if (alive) categories = items;
			})
			.catch(() => {
				if (alive) categoriesError = true;
			});
		return () => {
			alive = false;
		};
	});

	/* ---------- 预览 ---------- */

	async function onFilePicked(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const file = input.files?.[0];
		input.value = '';
		if (!file) return;

		fileName = file.name;
		error = '';
		// 契约 §6.2 文件上限：本地先拦一道，省一次上传往返
		if (file.size > IMPORT_MAX_BYTES) {
			error = '账单文件超过 5MB 限制，请按月份分开导出后再试';
			phase = 'pick';
			return;
		}

		phase = 'previewing';
		try {
			const preview = await api.previewImport(file, source);
			rows = preview.items;
			selected = defaultSelection(preview.items); // 疑似重复默认不勾
			overrides = {};
			rowIds = Object.fromEntries(preview.items.map((row) => [row.rowIndex, uuid()]));
			phase = 'preview';
		} catch (err) {
			error = previewErrorMessage(err, source);
			phase = 'pick';
		}
	}

	function reset() {
		phase = 'pick';
		rows = [];
		selected = [];
		overrides = {};
		rowIds = {};
		fileName = '';
		error = '';
		done = 0;
	}

	/* ---------- 勾选 / 改分类 ---------- */

	const isSelected = (rowIndex: number) => selected.includes(rowIndex);

	function toggle(rowIndex: number) {
		selected = toggleRow(selected, rowIndex);
	}

	function selectAll(next: boolean) {
		selected = next ? rows.map((row) => row.rowIndex) : [];
	}

	const rowCategoryId = (row: ImportRow) => overrides[row.rowIndex] ?? row.categoryId;

	function pickCategory(category: Category) {
		if (!editingRow) return;
		overrides = { ...overrides, [editingRow.rowIndex]: category.id };
		editingRow = null;
	}

	/* ---------- 导入：每行一条，全部走 outbox（离线安全 + 幂等） ---------- */

	async function runImport() {
		const picked = rows.filter((row) => isSelected(row.rowIndex));
		if (picked.length === 0) return;

		phase = 'importing';
		done = 0;
		let ok = 0;
		let failed = 0;

		// 分批：每批并发提交，批间让出主线程 → 进度条能画出来，几百行也不卡死
		for (const batch of chunk(picked, IMPORT_BATCH_SIZE)) {
			const settled = await Promise.allSettled(
				batch.map((row) =>
					outbox.createTransaction(
						toTransactionInput(row, rowIds[row.rowIndex] ?? uuid(), overrides[row.rowIndex])
					)
				)
			);
			ok += settled.filter((s) => s.status === 'fulfilled').length;
			failed += settled.filter((s) => s.status === 'rejected').length;
			done += batch.length;
			await new Promise((resolve) => setTimeout(resolve, 0));
		}

		result = { ok, failed, skipped: rows.length - picked.length };
		phase = 'done';
		// 汇总 toast 停留片刻后跳回账本（用户也可立即点按钮）
		setTimeout(() => {
			if (phase === 'done') void goto('/ledger');
		}, 1800);
	}
</script>

<svelte:head>
	<title>导入账单 · 拾光票局</title>
</svelte:head>

<header class="page-head">
	<a class="back" href="/me" aria-label="返回我的">← 我的</a>
	<h1>导入账单</h1>
</header>

{#if phase === 'pick'}
	<section class="card" aria-label="选择账单来源">
		<h2 class="card-title">1 · 账单来源</h2>
		<div class="sources" role="radiogroup" aria-label="账单来源">
			{#each IMPORT_SOURCES as s (s)}
				<button
					type="button"
					class="source-chip"
					class:selected={source === s}
					role="radio"
					aria-checked={source === s}
					onclick={() => (source = s)}
				>
					<span aria-hidden="true">{SOURCE_EMOJI[s]}</span>
					{SOURCE_LABEL[s]}
				</button>
			{/each}
		</div>
		<p class="hint">
			在{SOURCE_LABEL[source]}账单页导出 CSV（≤ 5MB），选好来源再选文件；解析只在本次进行，不会自动记账。
		</p>
	</section>

	<section class="card" aria-label="选择文件">
		<h2 class="card-title">2 · 选择 CSV 文件</h2>
		<Button block onclick={() => fileInput?.click()}>选择账单文件</Button>
		{#if fileName}
			<p class="hint file">已选：{fileName}</p>
		{/if}
		{#if error}
			<p class="error" role="alert">{error}</p>
		{/if}
	</section>

	<input
		bind:this={fileInput}
		type="file"
		accept=".csv,text/csv"
		class="file-hidden"
		onchange={onFilePicked}
		aria-hidden="true"
		tabindex="-1"
	/>
{:else if phase === 'previewing'}
	<div class="skeletons" aria-label="解析中">
		<Skeleton lines={1} height="64px" />
		<Skeleton lines={6} height="56px" />
	</div>
{:else if phase === 'preview'}
	{#if rows.length === 0}
		<EmptyState
			emoji="🧾"
			title="这份账单里没有可导入的记录"
			description="换一个时间段导出，或确认来源是否选对"
			actionLabel="重新选择"
			onaction={reset}
		/>
	{:else}
		<section class="card summary" aria-label="预览汇总">
			<div class="sum-row">
				<span class="sum-label">{SOURCE_EMOJI[source]} {SOURCE_LABEL[source]} · 共 {summary.total} 条</span>
				<button type="button" class="link" onclick={reset}>换文件</button>
			</div>
			<div class="sum-row">
				<span class="sum-label">
					已选 <b class="tnum">{summary.selected}</b> 条
					{#if summary.duplicates > 0}
						· 疑似重复 <b class="tnum">{summary.duplicates}</b> 条（默认不导入）
					{/if}
				</span>
				<div class="bulk">
					<button type="button" class="link" onclick={() => selectAll(true)}>全选</button>
					<button type="button" class="link" onclick={() => selectAll(false)}>全不选</button>
				</div>
			</div>
			<div class="sum-row totals">
				<span>支出 <Amount cents={summary.expenseCents} direction="expense" /></span>
				<span>收入 <Amount cents={summary.incomeCents} direction="income" /></span>
			</div>
			{#if categoriesError}
				<p class="error" role="alert">分类加载失败，将按建议分类导入（可稍后在账本中修改）</p>
			{/if}
		</section>

		<ul class="rows" aria-label="待导入的账目">
			{#each rows as row (row.rowIndex)}
				<li class="row" class:dup={row.duplicate} class:off={!isSelected(row.rowIndex)}>
					<button
						type="button"
						class="check"
						role="checkbox"
						aria-checked={isSelected(row.rowIndex)}
						aria-label="{isSelected(row.rowIndex) ? '取消导入' : '导入'}：{row.note ||
							'无备注'}"
						onclick={() => toggle(row.rowIndex)}
					>
						<span class="box" class:on={isSelected(row.rowIndex)} aria-hidden="true">✓</span>
					</button>

					<div class="row-main">
						<div class="row-top">
							<span class="note">{row.note || '无备注'}</span>
							<Amount cents={row.amountCents} direction={row.direction} />
						</div>
						<div class="row-bottom">
							<span class="time tnum">{fmtDateTime(row.occurredAt)}</span>
							<button
								type="button"
								class="cat"
								onclick={() => (editingRow = row)}
								aria-label="修改分类"
							>
								{categoryLabel(categories, rowCategoryId(row))} ▾
							</button>
							{#if row.duplicate}
								<span class="dup-tag">疑似重复</span>
							{/if}
						</div>
					</div>
				</li>
			{/each}
		</ul>

		<div class="submit-bar">
			<Button block disabled={summary.selected === 0} onclick={() => void runImport()}>
				{summary.selected === 0 ? '请至少勾选一条' : `导入 ${summary.selected} 条`}
			</Button>
		</div>
	{/if}
{:else if phase === 'importing'}
	<section class="card" aria-label="导入进度">
		<h2 class="card-title">正在导入…</h2>
		<div
			class="progress"
			role="progressbar"
			aria-valuemin="0"
			aria-valuemax="100"
			aria-valuenow={progressPct}
		>
			<div class="progress-fill" style:width="{progressPct}%"></div>
		</div>
		<p class="hint tnum">{done} / {summary.selected} 条 · 已写入本地，联网后自动同步</p>
	</section>
{:else}
	<section class="card" aria-label="导入完成">
		<EmptyState
			emoji="✅"
			title="导入完成"
			description="成功 {result.ok} 条 · 跳过 {result.skipped} 条{result.failed > 0
				? ` · 失败 ${result.failed} 条`
				: ''}"
			actionLabel="回到账本"
			onaction={() => void goto('/ledger')}
		/>
	</section>
	<div class="toast" role="status">
		已导入 {result.ok} 条，跳过 {result.skipped} 条{result.failed > 0 ? `，失败 ${result.failed} 条` : ''}
	</div>
{/if}

<!-- 改单行分类（复用 CategoryPicker） -->
<Sheet
	open={editingRow !== null}
	title="选择分类"
	onclose={() => (editingRow = null)}
>
	{#if editingRow}
		<CategoryPicker
			categories={categoriesFor(categories, editingRow.direction)}
			selected={rowCategoryId(editingRow)}
			onselect={pickCategory}
		/>
	{/if}
</Sheet>

<style>
	.page-head {
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

	.card {
		display: flex;
		flex-direction: column;
		gap: 12px;
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
		padding: 16px;
		margin-bottom: 16px;
	}

	.card-title {
		margin: 0;
		font-size: 0.875rem; /* 14 正文 */
		font-weight: 600;
		color: var(--ink-2);
	}

	.hint {
		margin: 0;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.file {
		overflow-wrap: anywhere;
	}

	.error {
		margin: 0;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--danger);
	}

	.file-hidden {
		display: none;
	}

	.skeletons {
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	/* ---- 来源选择 ---- */
	.sources {
		display: flex;
		gap: 8px;
	}

	.source-chip {
		flex: 1;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		gap: 4px;
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

	.source-chip.selected {
		border-color: var(--brand);
		background: color-mix(in srgb, var(--brand) 10%, transparent);
		color: var(--brand);
		font-weight: 600;
	}

	/* ---- 汇总卡 ---- */
	.summary {
		position: sticky;
		top: 0;
		z-index: 10;
	}

	.sum-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		font-size: 0.875rem;
		color: var(--ink);
	}

	.sum-label {
		color: var(--ink-2);
	}

	.sum-label b {
		color: var(--ink);
	}

	.totals {
		font-size: 0.75rem;
		color: var(--ink-2);
		gap: 16px;
		justify-content: flex-start;
	}

	.bulk {
		display: flex;
		gap: 8px;
		flex: none;
	}

	.link {
		min-height: 44px; /* 触控目标 */
		padding: 0 8px;
		border: none;
		background: transparent;
		color: var(--brand);
		font-family: inherit;
		font-size: 0.875rem;
		cursor: pointer;
	}

	/* ---- 行列表 ---- */
	.rows {
		list-style: none;
		margin: 0 0 96px; /* 给底部提交条留位 */
		padding: 0;
		background: var(--surface);
		border-radius: var(--radius-card);
		box-shadow: var(--shadow-card);
		overflow: hidden;
	}

	.row {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 12px 16px 12px 8px;
		transition: opacity var(--dur-fast) var(--ease);
	}

	.row + .row {
		border-top: 1px solid var(--line);
	}

	.row.off {
		opacity: 0.5;
	}

	.check {
		flex: none;
		display: grid;
		place-items: center;
		width: 44px; /* 触控目标 44×44 */
		height: 44px;
		border: none;
		background: transparent;
		cursor: pointer;
	}

	.box {
		display: grid;
		place-items: center;
		width: 22px;
		height: 22px;
		border: 1px solid var(--line);
		border-radius: var(--radius-chip);
		background: var(--surface);
		color: transparent;
		font-size: 0.75rem;
		line-height: 1;
		transition:
			background-color var(--dur-fast) var(--ease),
			border-color var(--dur-fast) var(--ease);
	}

	.box.on {
		background: var(--brand);
		border-color: var(--brand);
		color: var(--on-brand);
	}

	.row-main {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.row-top {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: 12px;
	}

	.note {
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.row-bottom {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}

	.time {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	/* 视觉 32px 高，触达区用 ::before 补到 44px（design §4 触控目标） */
	.cat {
		position: relative;
		min-height: 32px;
		padding: 4px 8px;
		border: 1px solid var(--line);
		border-radius: var(--radius-chip);
		background: transparent;
		color: var(--ink-2);
		font-family: inherit;
		font-size: 0.75rem;
		cursor: pointer;
	}

	.cat::before {
		content: '';
		position: absolute;
		inset: -6px;
	}

	.cat:active {
		border-color: var(--brand);
		color: var(--brand);
	}

	.dup-tag {
		font-size: 0.75rem;
		color: var(--danger);
		border: 1px solid color-mix(in srgb, var(--danger) 40%, transparent);
		background: color-mix(in srgb, var(--danger) 8%, transparent);
		border-radius: var(--radius-chip);
		padding: 2px 6px;
	}

	.row.dup .note {
		color: var(--ink-2);
	}

	/* ---- 底部提交条（安全区） ---- */
	.submit-bar {
		position: fixed;
		left: 0;
		right: 0;
		bottom: 0;
		z-index: 20;
		padding: 12px var(--page-inline)
			calc(env(safe-area-inset-bottom) + var(--tabbar-height) + 12px);
		background: var(--surface);
		border-top: 1px solid var(--line);
	}

	/* ---- 进度 ---- */
	.progress {
		height: 8px;
		border-radius: var(--radius-chip);
		background: var(--line);
		overflow: hidden;
	}

	.progress-fill {
		height: 100%;
		background: var(--brand);
		transition: width var(--dur-fast) var(--ease);
	}

	/* ---- 完成 toast ---- */
	.toast {
		position: fixed;
		left: var(--page-inline);
		right: var(--page-inline);
		bottom: calc(var(--tabbar-height) + env(safe-area-inset-bottom) + 16px);
		z-index: 30;
		padding: 12px 16px;
		border-radius: var(--radius-btn);
		background: var(--ink);
		color: var(--bg);
		font-size: 0.875rem;
		text-align: center;
		box-shadow: var(--shadow-card);
	}

	@media (min-width: 672px) {
		.submit-bar {
			left: 50%;
			right: auto;
			width: 640px;
			transform: translateX(-50%);
		}
	}
</style>
