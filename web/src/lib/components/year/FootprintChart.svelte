<script lang="ts">
	import { KIND_META } from '$lib/components/ticket/kinds';
	import type { PlaceVisit } from './data';

	interface Props {
		places: PlaceVisit[];
		/** 图内最多画几条，其余进「更多足迹」文本区 */
		top?: number;
	}

	let { places, top = 10 }: Props = $props();

	const shown = $derived(places.slice(0, top));
	const rest = $derived(places.slice(top));
	const maxCount = $derived(Math.max(1, ...shown.map((p) => p.count)));

	/* ---- 几何（viewBox 坐标系；容器高度随行数长，不裁 x 轴带） ---- */
	const W = 360;
	const ROW = 26;
	const BAR_H = 12; // ≤ 24px 厚
	const LABEL_W = 84;
	const VALUE_W = 30;
	const plotW = W - LABEL_W - VALUE_W - 8;
	const height = $derived(Math.max(ROW, shown.length * ROW));

	const barW = (count: number): number => Math.max(2, (count / maxCount) * plotW);

	/** 右端 4px 圆角、左端贴基线方正（dataviz mark 规范） */
	function barPath(i: number, count: number): string {
		const x = LABEL_W;
		const y = i * ROW + (ROW - BAR_H) / 2;
		const w = barW(count);
		const r = Math.min(4, w);
		return (
			`M ${x} ${y} L ${(x + w - r).toFixed(2)} ${y} ` +
			`Q ${(x + w).toFixed(2)} ${y} ${(x + w).toFixed(2)} ${(y + r).toFixed(2)} ` +
			`L ${(x + w).toFixed(2)} ${(y + BAR_H - r).toFixed(2)} ` +
			`Q ${(x + w).toFixed(2)} ${(y + BAR_H).toFixed(2)} ${(x + w - r).toFixed(2)} ${(y + BAR_H).toFixed(2)} ` +
			`L ${x} ${(y + BAR_H).toFixed(2)} Z`
		);
	}

	/** 悬停读数：整行命中（不要求指到柱上 —— dataviz 交互规范）。tooltip 只增强：
	    次数同时有柱尾直标与下方列表，不悬停也读得到。 */
	let svgEl = $state<SVGSVGElement | null>(null);
	let hovered = $state<string | null>(null);

	function pointAt(event: PointerEvent) {
		if (!svgEl || shown.length === 0) return;
		const rect = svgEl.getBoundingClientRect();
		const y = ((event.clientY - rect.top) / rect.height) * height;
		const i = Math.floor(y / ROW);
		hovered = i >= 0 && i < shown.length ? (shown[i]?.name ?? null) : null;
	}

	const clip = (name: string): string => (name.length > 6 ? `${name.slice(0, 5)}…` : name);
</script>

<div class="footprint">
	<svg
		bind:this={svgEl}
		viewBox="0 0 {W} {height}"
		style:height="{height}px"
		role="img"
		aria-label="城市足迹条形图，明细见下方列表"
		onpointermove={pointAt}
		onpointerdown={pointAt}
		onpointerleave={() => (hovered = null)}
	>
		{#each shown as p, i (p.name)}
			<text x={LABEL_W - 8} y={i * ROW + ROW / 2 + 4} class="name" text-anchor="end">{clip(p.name)}</text
			>
			<!-- 单系列 = 单色（--brand）；悬停行外的柱淡出 -->
			<path
				d={barPath(i, p.count)}
				class="bar"
				class:dim={hovered != null && hovered !== p.name}
				fill="var(--brand)"
			/>
			<!-- 选择性直标：次数就是唯一读数，标在柱尾 -->
			<text x={W - VALUE_W + 8} y={i * ROW + ROW / 2 + 4} class="value tnum">{p.count}</text>
		{/each}
	</svg>

	<!-- 表格视图：城市 · 票型 · 次数（读屏与无悬停可达） -->
	<ul class="list">
		{#each shown as p (p.name)}
			<li class="row" class:hot={hovered === p.name}>
				<span class="row-name">{p.name}</span>
				<span class="kinds">
					{#each p.kinds as kind (kind)}
						<span class="kind" style:background={KIND_META[kind].color} title={KIND_META[kind].label}>
							<span aria-hidden="true">{KIND_META[kind].emoji}</span>
							<span class="sr-only">{KIND_META[kind].label}</span>
						</span>
					{/each}
				</span>
				<span class="row-count tnum">{p.count} 次</span>
			</li>
		{/each}
	</ul>

	{#if rest.length > 0}
		<p class="rest">
			还有 {rest.length} 个地方：{rest.map((p) => p.name).join('、')}
		</p>
	{/if}
</div>

<style>
	.footprint {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	svg {
		display: block;
		width: 100%;
		touch-action: pan-y;
	}

	.name {
		font-size: 11px;
		fill: var(--ink);
	}

	.value {
		font-size: 11px;
		font-weight: 600;
		fill: var(--ink-2);
	}

	.bar {
		transition: opacity var(--dur-fast) var(--ease);
	}

	.dim {
		opacity: 0.45;
	}

	.list {
		list-style: none;
		margin: 0;
		padding: 0;
		border-top: 1px solid var(--line);
	}

	.row {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 44px; /* 触控/阅读行高 */
		padding: 0 4px;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--ink);
		border-bottom: 1px solid var(--line);
		transition: background-color var(--dur-fast) var(--ease);
	}

	.row.hot {
		background: var(--bg);
	}

	.row-name {
		flex: 1;
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.kinds {
		display: flex;
		gap: 4px;
	}

	.kind {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 20px;
		height: 20px;
		border-radius: var(--radius-chip);
		font-size: 11px;
		line-height: 1;
	}

	.row-count {
		flex: none;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.rest {
		margin: 0;
		font-size: 0.75rem;
		color: var(--ink-2);
	}

	.tnum {
		font-variant-numeric: tabular-nums;
	}

	.sr-only {
		position: absolute;
		width: 1px;
		height: 1px;
		padding: 0;
		margin: -1px;
		overflow: hidden;
		clip-path: inset(50%);
		white-space: nowrap;
	}
</style>
