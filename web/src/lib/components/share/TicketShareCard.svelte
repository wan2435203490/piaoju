<script lang="ts">
	/**
	 * 「分享票根」入口：按钮 → Sheet 内预览分享图 → 保存/分享。
	 * 图由 canvas 自绘（零新依赖，包体红线 conventions §5），
	 * 固定亮色纸感版式；触发按钮跟随主题（design §5）。
	 */
	import Button from '$lib/components/Button.svelte';
	import Sheet from '$lib/components/Sheet.svelte';
	import type { Ticket } from '$lib/api/types';
	import { renderShareImage, shareOrDownload, type ShareRender } from './render';
	import { SHARE_H, SHARE_W } from './layout';

	interface Props {
		ticket: Ticket;
	}

	let { ticket }: Props = $props();

	let open = $state(false);
	let rendering = $state(false);
	let saving = $state(false);
	let error = $state('');
	let result = $state<ShareRender | null>(null);
	let previewUrl = $state('');
	let hint = $state('');

	// 支持 navigator.share(File) 的环境（移动端 / Capacitor WebView）走系统分享面板
	const canSystemShare = (): boolean =>
		typeof navigator !== 'undefined' &&
		typeof File !== 'undefined' &&
		!!navigator.canShare?.({ files: [new File([], 'x.png', { type: 'image/png' })] });

	function reset() {
		if (previewUrl) URL.revokeObjectURL(previewUrl);
		previewUrl = '';
		result = null;
		error = '';
		hint = '';
	}

	async function generate() {
		reset();
		rendering = true;
		try {
			const render = await renderShareImage(ticket);
			result = render;
			previewUrl = URL.createObjectURL(render.blob);
			// 照片降级不是失败，只是版式变了 —— 明确告知，别让用户以为漏了照片
			hint = ticket.attachments.length > 0 && !render.withPhoto ? '票面照片加载失败，已生成无照片版式' : '';
		} catch (e) {
			error = e instanceof Error ? e.message : '生成失败，请重试';
		} finally {
			rendering = false;
		}
	}

	function start() {
		open = true;
		void generate();
	}

	async function save() {
		if (!result) return;
		saving = true;
		try {
			const outcome = await shareOrDownload(result.blob, result.fileName);
			if (outcome !== 'cancelled') open = false;
		} catch {
			error = '保存失败，请重试';
		} finally {
			saving = false;
		}
	}
</script>

<Button variant="ghost" block onclick={start}>生成分享图</Button>

<Sheet bind:open title="分享票根" onclose={reset}>
	<div class="share">
		{#if rendering}
			<div class="frame loading" style:aspect-ratio={`${SHARE_W} / ${SHARE_H}`}>
				<span class="spinner" aria-hidden="true"></span>
				<p class="muted" role="status">正在生成分享图…</p>
			</div>
		{:else if error}
			<div class="frame error" role="alert">
				<p class="err-emoji" aria-hidden="true">🌫️</p>
				<p class="err-text">{error}</p>
			</div>
			<Button variant="ghost" block onclick={generate}>重试</Button>
		{:else if result}
			<img class="preview" src={previewUrl} alt="{ticket.title} 的分享图预览" />
			{#if hint}
				<p class="muted" role="status">{hint}</p>
			{/if}
			<Button block loading={saving} onclick={save}>
				{canSystemShare() ? '分享图片' : '保存图片'}
			</Button>
		{/if}
	</div>
</Sheet>

<style>
	.share {
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding-block: 4px 8px;
	}

	.preview {
		display: block;
		width: 100%;
		border-radius: var(--radius-ticket);
		box-shadow: var(--shadow-card);
		/* 分享图固定亮色纸感：暗色下全局 img 降亮规则不应作用于它 */
		filter: none;
	}

	.frame {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 8px;
		min-height: 220px;
		border-radius: var(--radius-ticket);
		background: var(--bg);
		border: 1px solid var(--line);
	}

	.muted {
		margin: 0;
		text-align: center;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
	}

	.err-emoji {
		margin: 0;
		font-size: 1.75rem;
		line-height: 1;
	}

	.err-text {
		margin: 0;
		font-size: 0.875rem; /* 14 正文 */
		color: var(--danger);
		text-align: center;
	}

	.spinner {
		width: 20px;
		height: 20px;
		border: 2px solid var(--line);
		border-top-color: var(--brand);
		border-radius: 50%;
		animation: spin 600ms linear infinite;
	}

	@keyframes spin {
		to {
			transform: rotate(1turn);
		}
	}

	/* 全局 reduced-motion 规则已把 animation-duration 压到 0.01ms */
</style>
