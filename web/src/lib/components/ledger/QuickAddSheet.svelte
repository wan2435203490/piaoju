<script lang="ts">
	import Sheet from '$lib/components/Sheet.svelte';
	import NumPad, { type NumPadKey } from '$lib/components/NumPad.svelte';
	import CategoryPicker from '$lib/components/CategoryPicker.svelte';
	import type { Category, Direction, PaymentMethod, TransactionInput } from '$lib/api/types';
	import { PAYMENT_METHODS } from '$lib/api/types';
	import { formatCents } from '$lib/utils/money';
	import { uuid } from '$lib/utils/uuid';
	import { PAYMENT_LABELS } from './format';
	import { applyKey, evaluateCents, isCompound } from './expression';

	interface Props {
		/** 可 bind:open */
		open?: boolean;
		/** 全量分类（组件内按方向过滤） */
		categories: Category[];
		/** 分类加载失败：pool 为空时分类区显示错误态 + 重试（design §6） */
		categoriesError?: boolean;
		/** 重新拉取分类 */
		onretrycategories?: () => void;
		/**
		 * 保存回调：sheet 已自行关闭，页面负责乐观插入 + 走 outbox 落库
		 * （3 秒快记：保存即关，不等网络 —— design §4）。
		 */
		onsubmit?: (input: TransactionInput) => void;
	}

	let {
		open = $bindable(false),
		categories,
		categoriesError = false,
		onretrycategories,
		onsubmit
	}: Props = $props();

	/* ---- 快记草稿态（关闭不清空，保存后重置） ---- */
	let expr = $state('');
	let direction = $state<Direction>('expense');
	let note = $state('');
	let paymentMethod = $state<PaymentMethod>('wechat');
	/** 用户显式点过的分类；null = 跟随方向默认第一个（3 秒路径零点选） */
	let pickedCategory = $state<number | null>(null);
	let shaking = $state(false);

	const pool = $derived(categories.filter((c) => c.kind === direction));
	const categoryId = $derived(
		pickedCategory != null && pool.some((c) => c.id === pickedCategory)
			? pickedCategory
			: (pool[0]?.id ?? null)
	);
	const cents = $derived(evaluateCents(expr));
	const compound = $derived(isCompound(expr));

	function switchDirection(next: Direction) {
		if (direction === next) return;
		direction = next;
		pickedCategory = null; // 回落到新方向的默认分类
	}

	function onkey(key: NumPadKey) {
		expr = applyKey(expr, key);
	}

	function save() {
		// 全程唯一校验：金额 > 0（design §4）
		if (cents <= 0 || categoryId == null) {
			shaking = true;
			setTimeout(() => (shaking = false), 300);
			return;
		}
		const input: TransactionInput = {
			id: uuid(),
			amountCents: cents,
			direction,
			categoryId,
			note: note.trim(),
			occurredAt: new Date().toISOString(), // 日期默认今天、时间默认现在
			paymentMethod
		};
		// 保存即关；重置草稿，下次打开是干净键盘
		expr = '';
		note = '';
		pickedCategory = null;
		open = false;
		onsubmit?.(input);
	}
</script>

<Sheet bind:open title="快记">
	<!-- §4 自上而下：金额大数 → 分类网格 → NumPad → 备注（可跳过） -->
	<div class="amount-row" class:shake={shaking}>
		<div class="display" class:income={direction === 'income'} aria-live="polite">
			<span class="currency">¥</span>
			<span class="expr tnum" class:placeholder={expr === ''}>{expr === '' ? '0.00' : expr}</span>
			{#if compound}
				<span class="preview tnum">= {formatCents(cents)}</span>
			{/if}
		</div>
		<div class="seg" role="group" aria-label="收支方向">
			<button
				type="button"
				class="seg-btn expense"
				class:active={direction === 'expense'}
				aria-pressed={direction === 'expense'}
				onclick={() => switchDirection('expense')}>支出</button
			>
			<button
				type="button"
				class="seg-btn income"
				class:active={direction === 'income'}
				aria-pressed={direction === 'income'}
				onclick={() => switchDirection('income')}>收入</button
			>
		</div>
	</div>

	<div class="section">
		{#if pool.length > 0}
			<CategoryPicker categories={pool} selected={categoryId} onselect={(c) => (pickedCategory = c.id)} />
		{:else if categoriesError}
			<!-- 错误态：分类拉取失败时快记不可用，必须给出原因 + 重试（design §6） -->
			<div class="cat-fallback" role="alert">
				<p class="cat-hint">分类加载失败，暂时无法记账</p>
				<button type="button" class="cat-retry" onclick={() => onretrycategories?.()}>重试</button>
			</div>
		{:else}
			<!-- 加载态：分类由服务端预置，为空即尚未加载完成 -->
			<div class="cat-fallback" role="status">
				<p class="cat-hint">分类加载中…</p>
			</div>
		{/if}
	</div>

	<div class="section">
		<NumPad {onkey} ondone={save} doneLabel="保存" />
	</div>

	<div class="section extras">
		<input
			class="note"
			type="text"
			bind:value={note}
			maxlength={200}
			placeholder="备注（可跳过）"
			aria-label="备注"
		/>
		<div class="pay-row" role="group" aria-label="支付方式">
			{#each PAYMENT_METHODS as method (method)}
				<button
					type="button"
					class="chip"
					class:active={paymentMethod === method}
					aria-pressed={paymentMethod === method}
					onclick={() => (paymentMethod = method)}>{PAYMENT_LABELS[method]}</button
				>
			{/each}
		</div>
	</div>
</Sheet>

<style>
	.amount-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		padding: 4px 4px 12px;
	}

	.display {
		display: flex;
		align-items: baseline;
		gap: 4px;
		min-width: 0;
		color: var(--brand); /* 支出色 */
	}

	.display.income {
		color: var(--accent); /* 收入色 */
	}

	.currency {
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 600;
	}

	.expr {
		font-size: 1.75rem; /* 28 金额大数 */
		font-weight: 700;
		line-height: 1.25;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.expr.placeholder {
		color: var(--ink-2);
		font-weight: 400;
	}

	.preview {
		flex: none;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
	}

	.seg {
		display: flex;
		flex: none;
		padding: 2px;
		background: var(--bg);
		border-radius: var(--radius-btn);
	}

	.seg-btn {
		min-height: 44px; /* 触控目标 */
		min-width: 56px;
		padding: 0 10px;
		border: none;
		border-radius: calc(var(--radius-btn) - 2px);
		background: transparent;
		font-family: inherit;
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--ink-2);
		cursor: pointer;
		transition:
			background-color var(--dur-fast) var(--ease),
			color var(--dur-fast) var(--ease);
	}

	.seg-btn.active {
		background: var(--surface);
		box-shadow: var(--shadow-card);
	}

	.seg-btn.expense.active {
		color: var(--brand);
	}

	.seg-btn.income.active {
		color: var(--accent);
	}

	.section {
		padding-block: 8px;
	}

	/* ---- 分类区空/错误态 ---- */
	.cat-fallback {
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		padding: 16px 12px;
		background: var(--bg);
		border-radius: var(--radius-btn);
	}

	.cat-hint {
		margin: 0;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink-2);
		text-align: center;
	}

	.cat-retry {
		min-height: 44px; /* 触控目标 */
		min-width: 88px;
		padding: 0 16px;
		border: 1px solid var(--brand);
		border-radius: var(--radius-btn);
		background: transparent;
		font-family: inherit;
		font-size: 0.875rem;
		font-weight: 600;
		color: var(--brand);
		cursor: pointer;
		transition: background-color var(--dur-fast) var(--ease);
	}

	.cat-retry:active {
		background: color-mix(in srgb, var(--brand) 8%, transparent);
	}

	.extras {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.note {
		width: 100%;
		min-height: 44px; /* 触控目标 */
		padding: 0 12px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: var(--surface);
		font-family: inherit;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
	}

	.note::placeholder {
		color: var(--ink-2);
	}

	.pay-row {
		display: flex;
		gap: 8px;
		overflow-x: auto;
		-webkit-overflow-scrolling: touch;
	}

	.chip {
		flex: none;
		min-height: 44px; /* 触控目标 */
		padding: 0 14px;
		border: 1px solid var(--line);
		border-radius: var(--radius-btn);
		background: transparent;
		font-family: inherit;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		cursor: pointer;
		transition:
			border-color var(--dur-fast) var(--ease),
			color var(--dur-fast) var(--ease),
			background-color var(--dur-fast) var(--ease);
	}

	.chip.active {
		border-color: var(--brand);
		color: var(--brand);
		font-weight: 600;
		background: color-mix(in srgb, var(--brand) 8%, transparent);
	}

	.shake {
		animation: shake 0.3s var(--ease);
	}

	@keyframes shake {
		0%,
		100% {
			transform: translateX(0);
		}
		25% {
			transform: translateX(-4px);
		}
		75% {
			transform: translateX(4px);
		}
	}
</style>
