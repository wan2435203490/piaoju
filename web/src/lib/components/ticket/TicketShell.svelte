<script lang="ts">
	/**
	 * 票根卡外壳 —— 票根视觉语言的唯一实现（design skill §2）：
	 * - 左缘 4px 票型色条；movie 专属替换为 14px 胶片齿孔条
	 *   （radial-gradient 打出 --bg 色圆孔，background-size 14×12）
	 * - 撕票线：dashed border + 左右两个 radial-gradient 半圆打孔缺口（伪元素）
	 * - 有照片时 16:9 顶部裁切；无照片由正文区自行渲染票型图标（不留空白灰框）
	 * - 撕票线下方固定为「评分 + 感想」脚注
	 */
	import type { Snippet } from 'svelte';
	import type { TicketKind } from '$lib/api/types';
	import { KIND_META } from './kinds';
	import Rating from './Rating.svelte';

	interface Props {
		kind: TicketKind;
		/** 传入则整卡为链接（列表卡）；不传为静态容器（详情页顶卡） */
		href?: string;
		rating?: number;
		memo?: string;
		/** 票面照片（首张附件缩略图） */
		photoUrl?: string;
		photoAlt?: string;
		/** 正文区（撕票线上方） */
		children: Snippet;
	}

	let { kind, href, rating = 0, memo = '', photoUrl = '', photoAlt = '', children }: Props = $props();
</script>

<svelte:element
	this={href ? 'a' : 'article'}
	{href}
	class="shell"
	class:movie={kind === 'movie'}
	style:--kind-color={KIND_META[kind].color}
>
	{#if photoUrl}
		<img class="photo" src={photoUrl} alt={photoAlt} loading="lazy" />
	{/if}

	<div class="body">
		{@render children()}
	</div>

	<div class="tear" aria-hidden="true"></div>

	<div class="foot">
		{#if rating > 0}
			<Rating value={rating} />
		{/if}
		{#if memo}
			<p class="memo">“{memo}”</p>
		{:else if rating === 0}
			<p class="memo muted">还没写下感想</p>
		{/if}
	</div>
</svelte:element>

<style>
	.shell {
		/* 左缘色条宽度：普通票 4px，movie 胶片条 14px */
		--edge-w: 4px;
		--punch: 14px;
		position: relative;
		display: block;
		background: var(--surface);
		border-radius: var(--radius-ticket);
		box-shadow: var(--shadow-card);
		overflow: hidden; /* 裁掉色条/照片/打孔溢出，形成半圆缺口 */
		color: inherit;
		text-decoration: none;
		transition: transform var(--dur-fast) var(--ease);
	}

	a.shell:active {
		transform: scale(0.98);
	}

	/* 左缘票型色条（贯穿全高，压在照片之上） */
	.shell::before {
		content: '';
		position: absolute;
		inset-block: 0;
		left: 0;
		width: var(--edge-w);
		background-color: var(--kind-color);
		z-index: 1;
	}

	/* movie 专属：14px 胶片齿孔条（design §2 参数逐字落地） */
	.shell.movie {
		--edge-w: 14px;
	}

	.shell.movie::before {
		background-image: radial-gradient(circle, var(--bg) 2.5px, transparent 3px);
		background-size: 14px 12px;
		background-position: center;
	}

	.photo {
		display: block;
		width: 100%;
		aspect-ratio: 16 / 9;
		object-fit: cover;
	}

	.body {
		padding: 12px 16px 12px calc(var(--edge-w) + 12px);
	}

	/* 撕票线：dashed + 两侧 radial-gradient 半圆打孔（伪元素，不用图片） */
	.tear {
		position: relative;
		border-top: 1px dashed var(--line);
		z-index: 2;
	}

	.tear::before,
	.tear::after {
		content: '';
		position: absolute;
		top: calc(var(--punch) / -2);
		width: var(--punch);
		height: var(--punch);
		background: radial-gradient(
			circle,
			var(--bg) calc(var(--punch) / 2 - 1px),
			transparent calc(var(--punch) / 2)
		);
	}

	.tear::before {
		left: calc(var(--punch) / -2);
	}

	.tear::after {
		right: calc(var(--punch) / -2);
	}

	.foot {
		display: flex;
		align-items: center;
		gap: 8px;
		min-height: 36px;
		padding: 8px 16px 10px calc(var(--edge-w) + 12px);
	}

	.memo {
		margin: 0;
		font-size: 0.75rem; /* 12 辅助 */
		color: var(--ink-2);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.muted {
		opacity: 0.7;
	}
</style>
