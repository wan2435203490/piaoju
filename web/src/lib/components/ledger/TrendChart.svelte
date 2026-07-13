<script lang="ts">
	import type { DayStat } from '$lib/api/types';
	import { formatCents } from '$lib/utils/money';
	import { dayShort, monthDays, yuanCompact } from './format';

	interface Props {
		/** "YYYY-MM"，决定 x 轴天数 */
		month: string;
		/** 仅 expense 口径（PROTOCOL §7） */
		byDay: DayStat[];
	}

	let { month, byDay }: Props = $props();

	/* ---- 数据：铺满当月每一天，缺日补 0 ---- */
	interface Day {
		key: string;
		day: number;
		cents: number;
	}

	const days = $derived.by((): Day[] => {
		const map = new Map(byDay.map((d) => [d.date, d.expenseCents]));
		return monthDays(month).map((key, i) => ({ key, day: i + 1, cents: map.get(key) ?? 0 }));
	});

	/* ---- 几何（viewBox 坐标系） ---- */
	const W = 360;
	const H = 180;
	const PAD_L = 42;
	const PAD_R = 6;
	const PAD_T = 16;
	const PAD_B = 20;
	const plotW = W - PAD_L - PAD_R;
	const plotH = H - PAD_T - PAD_B;
	const baseline = PAD_T + plotH;

	const maxCents = $derived(Math.max(0, ...days.map((d) => d.cents)));

	/** 干净刻度：步长取 1/2/5×10ⁿ，3-5 条（dataviz「clean numbers」） */
	const ticks = $derived.by((): number[] => {
		const maxYuan = Math.max(1, maxCents / 100);
		const pow = Math.pow(10, Math.floor(Math.log10(maxYuan / 3)));
		const step =
			[1, 2, 5, 10].map((m) => m * pow).find((s) => Math.ceil(maxYuan / s) <= 4) ?? 10 * pow;
		const top = Math.ceil(maxYuan / step) * step;
		const out: number[] = [];
		for (let v = 0; v <= top; v += step) out.push(v);
		return out;
	});

	const topYuan = $derived(ticks[ticks.length - 1] ?? 1);
	const yOf = (cents: number): number => baseline - (cents / (topYuan * 100)) * plotH;

	const slot = $derived(plotW / days.length);
	const barW = $derived(Math.min(24, Math.max(3, slot - 2))); // ≤24px 厚 + 2px surface 间隙

	/** 顶端 4px 圆角、贴基线方正（dataviz mark 规范） */
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

	const maxIndex = $derived(maxCents > 0 ? days.findIndex((d) => d.cents === maxCents) : -1);
	const xLabels = [1, 8, 15, 22, 29];

	/* ---- 交互：最近柱读数（命中不要求指到柱上 —— dataviz 交互规范） ---- */
	let svgEl = $state<SVGSVGElement | null>(null);
	let hovered = $state<number | null>(null);

	function pointAt(event: PointerEvent) {
		if (!svgEl) return;
		const rect = svgEl.getBoundingClientRect();
		const x = ((event.clientX - rect.left) / rect.width) * W;
		const i = Math.floor((x - PAD_L) / slot);
		hovered = i >= 0 && i < days.length ? i : null;
	}

	/** 读数行：悬停日优先，否则默认最高日 */
	const readout = $derived.by((): Day | null => {
		if (hovered != null) return days[hovered] ?? null;
		if (maxIndex >= 0) return days[maxIndex] ?? null;
		return null;
	});

	/** 直标不出画布：夹在绘图区内 */
	const clampX = (x: number): number => Math.min(PAD_L + plotW - 14, Math.max(PAD_L + 14, x));
</script>

<div class="trend">
	{#if readout}
		<div class="readout">
			<span class="date">{dayShort(readout.key)}{hovered == null ? ' · 当月最高' : ''}</span>
			<span class="value tnum">¥{formatCents(readout.cents)}</span>
		</div>
	{/if}

	<svg
		bind:this={svgEl}
		viewBox="0 0 {W} {H}"
		role="img"
		aria-label="每日支出柱状图，数值见后面的表格"
		onpointermove={pointAt}
		onpointerdown={pointAt}
		onpointerleave={() => (hovered = null)}
	>
		<!-- 网格：一档灰、1px、实线（hairline，禁虚线） -->
		{#each ticks as tick (tick)}
			<line x1={PAD_L} x2={PAD_L + plotW} y1={yOf(tick * 100)} y2={yOf(tick * 100)} class="grid" />
			<text x={PAD_L - 6} y={yOf(tick * 100) + 3} class="tick tnum" text-anchor="end"
				>{yuanCompact(tick * 100)}</text
			>
		{/each}

		<!-- 柱：单系列 = 单色（--brand 支出色）；悬停时其余淡出 -->
		{#each days as d, i (d.key)}
			{#if d.cents > 0}
				<path
					d={barPath(i, d.cents)}
					class="bar"
					class:dim={hovered != null && hovered !== i}
					fill="var(--brand)"
				/>
			{/if}
		{/each}

		<!-- 选择性直标：只标最高日（dataviz：never a number on every point） -->
		{#if maxIndex >= 0 && hovered == null}
			<text
				x={clampX(barCenter(maxIndex))}
				y={yOf(maxCents) - 5}
				class="peak tnum"
				text-anchor="middle">¥{yuanCompact(maxCents)}</text
			>
		{/if}

		<!-- x 轴：稀疏日刻度 -->
		{#each xLabels as day (day)}
			{#if day <= days.length}
				<text x={barCenter(day - 1)} y={H - 6} class="tick tnum" text-anchor="middle">{day}</text>
			{/if}
		{/each}
	</svg>

	<!-- 表格视图（读屏/无悬停可达 —— tooltip 只增强不设卡） -->
	<table class="sr-only">
		<caption>每日支出明细</caption>
		<thead><tr><th scope="col">日期</th><th scope="col">支出</th></tr></thead>
		<tbody>
			{#each days.filter((d) => d.cents > 0) as d (d.key)}
				<tr><td>{dayShort(d.key)}</td><td>¥{formatCents(d.cents)}</td></tr>
			{/each}
		</tbody>
	</table>
</div>

<style>
	.trend {
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

	.date {
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
		touch-action: pan-y; /* 纵向滚动不被图表拦截 */
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
