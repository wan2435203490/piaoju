<script lang="ts">
	let { children } = $props();
</script>

<!-- 认证页：无 TabBar 的窄布局 -->
<main class="auth">
	<div class="brand" aria-hidden="true">
		<span class="mark">🎫</span>
		<span class="name">拾光票局</span>
	</div>
	{@render children()}
</main>

<style>
	.auth {
		max-width: 400px;
		min-height: 100dvh;
		margin-inline: auto;
		padding: 48px var(--page-inline) calc(24px + env(safe-area-inset-bottom));
		display: flex;
		flex-direction: column;
	}

	.brand {
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 8px;
		padding-block: 24px;
	}

	.mark {
		font-size: 1.75rem;
		line-height: 1;
	}

	.name {
		font-size: 1.25rem; /* 20 标题 */
		font-weight: 700;
		color: var(--ink);
		letter-spacing: 0.05em;
	}
</style>
