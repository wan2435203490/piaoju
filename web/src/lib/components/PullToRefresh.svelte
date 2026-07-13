<script lang="ts">
	/**
	 * 下拉刷新（design §4：列表页必配三件套之一）。
	 * 页面滚动到顶部后继续下拉，超过阈值松手触发 onrefresh，
	 * resolve 后指示器收回。用指示器占位高度而非 transform 推开内容，
	 * 避免 transform 祖先改变 fixed 元素（FAB/Sheet/Toast）的包含块。
	 */
	import type { Snippet } from 'svelte';

	let {
		onrefresh,
		children
	}: {
		/** 松手触发的刷新回调 */
		onrefresh: () => Promise<void> | void;
		children: Snippet;
	} = $props();

	const THRESHOLD = 64; // 触发阈值 px
	const MAX_PULL = 96; // 最大下拉距离 px（0.4 阻尼后）

	let pull = $state(0);
	let pulling = $state(false);
	let refreshing = $state(false);
	let startY = 0;
	let armed = false; // 手势起点是否在页面顶部

	function atTop(): boolean {
		return (window.scrollY || document.documentElement.scrollTop) <= 0;
	}

	function handleStart(e: TouchEvent) {
		if (refreshing) return;
		armed = atTop();
		startY = e.touches[0].clientY;
		pulling = true;
	}

	function handleMove(e: TouchEvent) {
		if (!pulling || refreshing) return;
		if (!armed) {
			// 起手不在顶部：滚到顶后从当前位置重新起算
			if (!atTop()) return;
			armed = true;
			startY = e.touches[0].clientY;
			return;
		}
		const dy = e.touches[0].clientY - startY;
		pull = dy > 0 && atTop() ? Math.min(MAX_PULL, dy * 0.4) : 0;
	}

	async function handleEnd() {
		if (!pulling) return;
		pulling = false;
		if (pull >= THRESHOLD && !refreshing) {
			refreshing = true;
			pull = THRESHOLD; // 刷新期间指示器保持展开
			try {
				await onrefresh();
			} finally {
				refreshing = false;
				pull = 0;
			}
		} else {
			pull = 0;
		}
	}

	const ready = $derived(pull >= THRESHOLD);
</script>

<!-- 手势容器本身无语义（role=presentation），状态经 indicator 的 role=status 播报 -->
<div
	class="ptr"
	class:pulling
	role="presentation"
	ontouchstart={handleStart}
	ontouchmove={handleMove}
	ontouchend={handleEnd}
	ontouchcancel={handleEnd}
>
	<div
		class="indicator"
		role="status"
		aria-label={refreshing ? '刷新中' : ready ? '松开刷新' : '下拉刷新'}
		style:height="{pull}px"
	>
		<span
			class="spinner"
			class:spin={refreshing}
			style:opacity={refreshing ? 1 : Math.min(1, pull / THRESHOLD)}
			aria-hidden="true"
		></span>
	</div>
	{@render children()}
</div>

<style>
	.indicator {
		display: grid;
		place-items: center;
		height: 0;
		overflow: hidden;
	}

	/* 跟手时不加过渡；松手回弹/收回时动画 */
	.ptr:not(.pulling) .indicator {
		transition: height var(--dur-fast) var(--ease);
	}

	.spinner {
		width: 22px;
		height: 22px;
		border: 2px solid var(--line);
		border-top-color: var(--brand);
		border-radius: 50%;
	}

	.spinner.spin {
		animation: ptr-spin 800ms linear infinite;
	}

	@keyframes ptr-spin {
		to {
			transform: rotate(360deg);
		}
	}

	@media (prefers-reduced-motion: reduce) {
		.ptr:not(.pulling) .indicator {
			transition: none;
		}

		/* 加载指示保留旋转但放缓，避免完全静止无反馈 */
		.spinner.spin {
			animation-duration: 1600ms;
		}
	}
</style>
