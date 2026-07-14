<script lang="ts">
	import { yuanCompact } from '$lib/components/ledger/format';
	import { formatCents } from '$lib/utils/money';
	import type { MonthPoint } from './data';

	interface Props {
		/** 12 个点（缺月已在数据层降级为 0） */
		points: MonthPoint[];
	}

	let { points }: Props = $props();

	/* ---- 几何（viewBox 坐标系；容器含 x 轴带，不产生内滚动） ---- */
	const W = 360;
	const H = 200;
	const PAD_L = 42;
	const PAD_R = 6;
	const PAD_T = 16;
	const PAD_B = 22;
	const plotW = W - PAD_L - PAD_R;
	const plotH = H - PAD_T - PAD_B;
	const baseline = PAD_T + plotH;

	const maxCents = $derived(Math.max(0, ...points.map((p) => p.cents)));

	/** 干净刻度：步长 1/2/5×10ⁿ，≤4 段（dataviz「clean numbers」） */
	const ticks = $derived.by((): number[] => {
		const maxYuan = Math.max(1, maxCents / 100);
		const pow = Math.pow(10, Math.floor(Math.log10(maxYuan / 3)));
		const step =
			[1, 2, 5, 10].map((m) => m * pow).find((s) => Math.ceil(maxYuan / s) <= 4) ?? 10 * pow;
		const topYuan = Math.ceil(maxYuan / step) * step;
		const out: number[] = [];
		for (let v = 0; v <= topYuan; v += step) out.push(v);
		return out;
	});

	const topYuan = $derived(ticks[ticks.length - 1] ?? 1);
	const yOf = (cents: number): number => baseline - (cents / (topYuan * 100)) * plotH;

	const slot = $derived(plotW / Math.max(1, points.length));
	const barW = $derived(Math.min(24, Math.max(4, slot - 6))); // ≤24px 厚 + surface 间隙

	/** 顶端 4px 圆角、贴基线方正 */
	function barPath(i: number, cents: number): string {
		const x = PAD_L + i * slot + (slot - barW) / 2;
		const yt = yOf(cents);
		const r = Math.min(4, barW / 2, baseline - yt);
		return (
			`M ${x.toFixed(2)} ${baseline} L ${x.toFixed(2)} ${(yt + r).toFixed(2)} ` +
			`Q ${x.toFixed(2)} ${yt.toFixed(2)} ${(x + r).toFixed(2)} ${yt.toFixed(2)} ` +
			`L ${(x + barW - r).toFixed(2)} ${yt.toFixed(2)} ` +
			`Q ${(x + barW).toFixed(2)} ${yt.toFixed(2)} ${(x + barW).toFixed(2)} ${(yt + r).toFixed(2)} ` +
			`L ${(x + barW).toFixed(2)} ${baseline} Z`
		);
	}

	const barCenter = (i: number): number => PAD_L + i * slot + slot / 2;

	const maxIndex = $derived(maxCents > 0 ? points.findIndex((p) => p.cents === maxCents) : -1);

	/* ---- 交互：最近柱读数（命中不要求指到柱上） ---- */
	let svgEl = $state<SVGSVGElement | null>(null);
	let hovered = $state<number | null>(null);

	function pointAt(event: PointerEvent) {
		if (!svgEl) return;
		const rect = svgEl.getBoundingClientRect();
		const x = ((event.clientX - rect.left) / rect.width) * W;
		const i = Math.floor((x - PAD_L) / slot);
		hovered = i >= 0 && i < points.length ? i : null;
	}

	/** 读数行：悬停月优先，否则默认最高月 */
	const readout = $derived.by((): MonthPoint | null => {
		if (hovered != null) return points[hovered] ?? null;
		if (maxIndex >= 0) return points[maxIndex] ?? null;
		return null;
	});

	const clampX = (x: number): number => Math.min(PAD_L + plotW - 16, Math.max(PAD_L + 16, x));
</script>

<div class="chart">
	{#if readout}
		<div class="readout">
			<span class="label tnum">{readout.index} 月{hovered == null ? ' · 全年最高' : ''}</span>
			<span class="value tnum">¥{formatCents(readout.cents)}</span>
		</div>
	{/if}

	<svg
		bind:this={svgEl}
		viewBox="0 0 {W} {H}"
		role="img"
		aria-label="月度支出柱状图，数值见后面的表格"
		onpointermove={pointAt}
		onpointerdown={pointAt}
		onpointerleave={() => (hovered = null)}
	>
		<!-- 网格：一档灰、1px、实线 -->
		{#each ticks as tick (tick)}
			<line x1={PAD_L} x2={PAD_L + plotW} y1={yOf(tick * 100)} y2={yOf(tick * 100)} class="grid" />
			<text x={PAD_L - 6} y={yOf(tick * 100) + 3} class="tick tnum" text-anchor="end"
				>{yuanCompact(tick * 100)}</text
			>
		{/each}

		<!-- 柱：单系列 = 单色（--brand 支出色，不做「越大越深」的值域上色） -->
		{#each points as p, i (p.month)}
			{#if p.cents > 0}
				<path
					d={barPath(i, p.cents)}
					class="bar"
					class:dim={hovered != null && hovered !== i}
					fill="var(--brand)"
				/>
			{/if}
		{/each}

		<!-- 选择性直标：只标最高月 -->
		{#if maxIndex >= 0 && hovered == null}
			<text x={clampX(barCenter(maxIndex))} y={yOf(maxCents) - 5} class="peak tnum" text-anchor="middle"
				>¥{yuanCompact(maxCents)}</text
			>
		{/if}

		<!-- x 轴：12 个月刻度 -->
		{#each points as p, i (p.month)}
			<text x={barCenter(i)} y={H - 6} class="tick tnum" text-anchor="middle">{p.index}</text>
		{/each}
	</svg>

	<!-- 表格视图（读屏 / 无悬停可达） -->
	<table class="sr-only">
		<caption>月度支出明细</caption>
		<thead><tr><th scope="col">月份</th><th scope="col">支出</th></tr></thead>
		<tbody>
			{#each points as p (p.month)}
				<tr><td>{p.index} 月</td><td>¥{formatCents(p.cents)}</td></tr>
			{/each}
		</tbody>
	</table>
</div>

<style>
	.chart {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.readout {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		min-height: 24px;
		padding-inline: 4px;
	}

	.label {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.value {
		font-size: 1rem; /* 16 强调 */
		font-weight: 600;
		color: var(--ink);
	}

	svg {
		display: block;
		width: 100%;
		height: auto;
		touch-action: pan-y;
	}

	.grid {
		stroke: var(--line);
		stroke-width: 1;
	}

	.bar {
		transition: opacity var(--dur-fast) var(--ease);
	}

	.dim {
		opacity: 0.45;
	}

	.tick {
		font-size: 10px;
		fill: var(--ink-2);
	}

	.peak {
		font-size: 10px;
		font-weight: 600;
		fill: var(--ink);
	}

	.tnum {
		font-variant-numeric: tabular-nums;
	}
</style>
