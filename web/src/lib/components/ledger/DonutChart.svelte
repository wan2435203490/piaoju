<script lang="ts" module>
	/** 环形图切片（页面按 dataviz 规则装配：≤6 段，尾部折叠为「其余」） */
	export interface DonutSlice {
		/** categoryId；-1 = 折叠的其余分类 */
		key: number;
		label: string;
		cents: number;
		count: number;
		/** CSS 颜色（一律 design token，如 var(--kind-movie)） */
		color: string;
	}
</script>

<script lang="ts">
	import { formatCents } from '$lib/utils/money';

	interface Props {
		slices: DonutSlice[];
		/** 中心默认展示的合计（本月支出） */
		totalCents: number;
		/** 选中切片 key；可 bind（图例行与切片共用同一选中态） */
		selected?: number | null;
	}

	let { slices, totalCents, selected = $bindable(null) }: Props = $props();

	/* ---- 几何：stroke 圆弧 + 2px surface 间隙（dataviz mark 规范） ---- */
	const SIZE = 220;
	const C = SIZE / 2;
	const R = 78;
	const STROKE = 30;
	const GAP_RAD = 2 / R; // 相邻切片间 2px 弧长的留白

	const sum = $derived(slices.reduce((acc, s) => acc + s.cents, 0));

	interface Arc extends DonutSlice {
		d: string;
		percent: number;
	}

	function point(rad: number): [number, number] {
		// 12 点钟方向起画，顺时针
		const a = rad - Math.PI / 2;
		return [C + R * Math.cos(a), C + R * Math.sin(a)];
	}

	const arcs = $derived.by((): Arc[] => {
		if (sum <= 0) return [];
		let acc = 0;
		return slices.map((s) => {
			const span = (s.cents / sum) * Math.PI * 2;
			const pad = slices.length > 1 ? Math.min(GAP_RAD, span * 0.4) / 2 : 0;
			const a0 = acc + pad;
			const a1 = acc + span - pad;
			acc += span;
			const [x0, y0] = point(a0);
			const [x1, y1] = point(a1);
			const large = a1 - a0 > Math.PI ? 1 : 0;
			return {
				...s,
				percent: (s.cents / sum) * 100,
				d: `M ${x0.toFixed(2)} ${y0.toFixed(2)} A ${R} ${R} 0 ${large} 1 ${x1.toFixed(2)} ${y1.toFixed(2)}`
			};
		});
	});

	const active = $derived(arcs.find((a) => a.key === selected) ?? null);

	function toggle(key: number) {
		selected = selected === key ? null : key;
	}

	function onSliceKey(event: KeyboardEvent, key: number) {
		if (event.key === 'Enter' || event.key === ' ') {
			event.preventDefault();
			toggle(key);
		}
	}
</script>

<!-- 键盘/读屏走页面图例列表（同一选中态）；圆环本体为指针快捷入口 -->
<div class="donut">
	<svg viewBox="0 0 {SIZE} {SIZE}" role="img" aria-label="本月支出分类占比环形图，数值见下方图例列表">
		{#if arcs.length === 1 && arcs[0]}
			<circle
				cx={C}
				cy={C}
				r={R}
				fill="none"
				stroke={arcs[0].color}
				stroke-width={STROKE}
				class="slice"
				role="button"
				tabindex={-1}
				aria-label="{arcs[0].label} ¥{formatCents(arcs[0].cents)}"
				onclick={() => arcs[0] && toggle(arcs[0].key)}
				onkeydown={(e) => arcs[0] && onSliceKey(e, arcs[0].key)}
			/>
		{:else}
			{#each arcs as arc (arc.key)}
				<path
					d={arc.d}
					fill="none"
					stroke={arc.color}
					stroke-width={STROKE}
					stroke-linecap="butt"
					class="slice"
					class:dim={selected != null && selected !== arc.key}
					role="button"
					tabindex={-1}
					aria-label="{arc.label} ¥{formatCents(arc.cents)} 占 {arc.percent.toFixed(1)}%"
					onclick={() => toggle(arc.key)}
					onkeydown={(e) => onSliceKey(e, arc.key)}
				/>
			{/each}
		{/if}
	</svg>
	<div class="center" aria-hidden="true">
		{#if active}
			<span class="label">{active.label} · {active.percent.toFixed(1)}%</span>
			<span class="value tnum">¥{formatCents(active.cents)}</span>
		{:else}
			<span class="label">本月支出</span>
			<span class="value tnum">¥{formatCents(totalCents)}</span>
		{/if}
	</div>
</div>

<style>
	.donut {
		position: relative;
		max-width: 240px;
		margin-inline: auto;
	}

	svg {
		display: block;
		width: 100%;
		height: auto;
	}

	.slice {
		cursor: pointer;
		transition: opacity var(--dur-fast) var(--ease);
		outline: none;
	}

	.dim {
		opacity: 0.35;
	}

	.center {
		position: absolute;
		inset: 0;
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 2px;
		pointer-events: none;
		text-align: center;
		padding: 48px;
	}

	.label {
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
		max-width: 100%;
	}

	.value {
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 700;
		color: var(--ink);
	}
</style>
