<script lang="ts">
	import '../app.css';
	import favicon from '$lib/assets/favicon.svg';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
	<title>拾光票局</title>
</svelte:head>

<!-- SafeArea：顶部/左右安全区在壳上统一处理；底部由 TabBar / 各页自理 -->
<div class="app-shell">
	{@render children()}
</div>

<style>
	.app-shell {
		min-height: 100dvh;
		padding-top: env(safe-area-inset-top);
		padding-left: env(safe-area-inset-left);
		padding-right: env(safe-area-inset-right);
	}
</style>
