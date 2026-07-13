<script lang="ts">
	import { addMonths, currentMonth, monthTitle } from './format';

	interface Props {
		/** "YYYY-MM" */
		value: string;
		onchange?: (month: string) => void;
		/** 可切换到的最晚月份，默认本月（账本/统计都不看未来） */
		max?: string;
	}

	let { value, onchange, max = currentMonth() }: Props = $props();

	const atMax = $derived(value >= max);

	function shift(delta: number) {
		const next = addMonths(value, delta);
		if (next > max) return;
		onchange?.(next);
	}
</script>

<div class="switcher" role="group" aria-label="切换月份">
	<button type="button" class="nav" aria-label="上一月" onclick={() => shift(-1)}>
		<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M10 3 L5 8 L10 13" /></svg>
	</button>
	<span class="label tnum" aria-live="polite">{monthTitle(value)}</span>
	<button type="button" class="nav" aria-label="下一月" disabled={atMax} onclick={() => shift(1)}>
		<svg viewBox="0 0 16 16" aria-hidden="true"><path d="M6 3 L11 8 L6 13" /></svg>
	</button>
</div>

<style>
	.switcher {
		display: inline-flex;
		align-items: center;
		gap: 4px;
	}

	.nav {
		display: grid;
		place-items: center;
		width: 44px; /* 触控目标 ≥ 44px */
		height: 44px;
		border: none;
		border-radius: var(--radius-btn);
		background: transparent;
		color: var(--ink-2);
		cursor: pointer;
		transition:
			background-color var(--dur-fast) var(--ease),
			color var(--dur-fast) var(--ease);
	}

	.nav:active:not(:disabled) {
		background: var(--line);
	}

	.nav:disabled {
		opacity: 0.35;
		cursor: not-allowed;
	}

	.nav svg {
		width: 16px;
		height: 16px;
		fill: none;
		stroke: currentColor;
		stroke-width: 2;
		stroke-linecap: round;
		stroke-linejoin: round;
	}

	.label {
		min-width: 88px;
		text-align: center;
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
	}
</style>
