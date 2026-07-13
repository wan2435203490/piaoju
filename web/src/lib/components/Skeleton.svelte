<script lang="ts">
	interface Props {
		/** 占位条数量；1 条时宽高完全由 width/height 决定 */
		lines?: number;
		/** 单条高度 */
		height?: string;
		width?: string;
	}

	let { lines = 3, height = '16px', width = '100%' }: Props = $props();
</script>

<!-- 脉冲占位条：首屏加载 > 300ms 的列表页必配（design §4） -->
<div class="skeleton" aria-hidden="true">
	{#each Array(lines) as _, i (i)}
		<div
			class="bar"
			style:height
			style:width={lines > 1 && i === lines - 1 ? '60%' : width}
		></div>
	{/each}
</div>

<style>
	.skeleton {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.bar {
		background: var(--line);
		border-radius: var(--radius-chip);
		animation: pulse 1.2s var(--ease) infinite alternate;
	}

	@keyframes pulse {
		from {
			opacity: 1;
		}
		to {
			opacity: 0.45;
		}
	}
</style>
