<script lang="ts" module>
	/** 按键值：数字 / 小数点 / 加号（键盘即算式）/ 退格 */
	export type NumPadKey =
		| '0'
		| '1'
		| '2'
		| '3'
		| '4'
		| '5'
		| '6'
		| '7'
		| '8'
		| '9'
		| '.'
		| '+'
		| 'backspace';
</script>

<script lang="ts">
	interface Props {
		/** 每次按键回调（算式/金额逻辑由调用方实现 —— W2 接手） */
		onkey?: (key: NumPadKey) => void;
		/** 「完成」回调 */
		ondone?: () => void;
		doneLabel?: string;
		disabled?: boolean;
	}

	let { onkey, ondone, doneLabel = '完成', disabled = false }: Props = $props();

	const press = (key: NumPadKey) => () => onkey?.(key);
</script>

<!-- 4×4 网格壳：1-9 / . / 0（双宽）/ ⌫ / ＋ / 完成（双高） -->
<div class="numpad" role="group" aria-label="数字键盘">
	<button type="button" class="key" style:grid-area="k1" {disabled} onclick={press('1')}>1</button>
	<button type="button" class="key" style:grid-area="k2" {disabled} onclick={press('2')}>2</button>
	<button type="button" class="key" style:grid-area="k3" {disabled} onclick={press('3')}>3</button>
	<button type="button" class="key fn" style:grid-area="del" {disabled} onclick={press('backspace')} aria-label="退格">⌫</button>
	<button type="button" class="key" style:grid-area="k4" {disabled} onclick={press('4')}>4</button>
	<button type="button" class="key" style:grid-area="k5" {disabled} onclick={press('5')}>5</button>
	<button type="button" class="key" style:grid-area="k6" {disabled} onclick={press('6')}>6</button>
	<button type="button" class="key fn" style:grid-area="plus" {disabled} onclick={press('+')} aria-label="加号">＋</button>
	<button type="button" class="key" style:grid-area="k7" {disabled} onclick={press('7')}>7</button>
	<button type="button" class="key" style:grid-area="k8" {disabled} onclick={press('8')}>8</button>
	<button type="button" class="key" style:grid-area="k9" {disabled} onclick={press('9')}>9</button>
	<button type="button" class="key done" style:grid-area="done" {disabled} onclick={() => ondone?.()}>{doneLabel}</button>
	<button type="button" class="key" style:grid-area="dot" {disabled} onclick={press('.')}>.</button>
	<button type="button" class="key" style:grid-area="k0" {disabled} onclick={press('0')}>0</button>
</div>

<style>
	.numpad {
		display: grid;
		grid-template-areas:
			'k1 k2 k3 del'
			'k4 k5 k6 plus'
			'k7 k8 k9 done'
			'dot k0 k0 done';
		grid-template-columns: repeat(4, 1fr);
		gap: 8px;
		touch-action: manipulation;
	}

	.key {
		min-height: 52px; /* 触控目标 ≥ 44px */
		border: none;
		border-radius: var(--radius-btn);
		background: var(--bg);
		color: var(--ink);
		font-family: inherit;
		font-size: 1.25rem; /* 20 标题档，键盘数字 */
		font-weight: 600;
		font-variant-numeric: tabular-nums;
		cursor: pointer;
		transition:
			background-color var(--dur-fast) var(--ease),
			transform var(--dur-fast) var(--ease);
		-webkit-user-select: none;
		user-select: none;
	}

	.key:active:not(:disabled) {
		background: var(--line);
		transform: scale(0.96);
	}

	.key:disabled {
		opacity: 0.5;
		cursor: not-allowed;
	}

	.fn {
		color: var(--brand);
		font-size: 1.125rem;
	}

	.done {
		background: var(--brand);
		color: var(--on-brand);
		font-size: 1rem; /* 16 强调 */
	}

	.done:active:not(:disabled) {
		background: var(--brand);
		opacity: 0.9;
	}
</style>
