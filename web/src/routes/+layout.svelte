<script lang="ts">
	import { onMount } from 'svelte';
	import '../app.css';
	import favicon from '$lib/assets/favicon.svg';

	let { children } = $props();

	// 注册 Service Worker（app shell 缓存 → 断网也能打开；见 src/service-worker.ts）。
	// dev 模式不注册：SW 会缓存住 HMR 资源，改代码看不到效果。
	onMount(() => {
		if (import.meta.env.DEV) return;
		if (!('serviceWorker' in navigator)) return;
		navigator.serviceWorker.register('/service-worker.js', { type: 'module' }).catch((err) => {
			console.warn('[piaoju] service worker 注册失败（离线打开将不可用）', err);
		});
	});
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
