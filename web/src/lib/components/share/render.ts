/**
 * 票根分享图 —— 浏览器侧（canvas / 跨域图片 / 导出）。
 * 纯计算在 layout.ts、绘制在 draw.ts，这里只做副作用。
 *
 * 跨域：附件是签名 URL，必须 crossOrigin='anonymous' 才不污染 canvas。
 * 任何一环失败（加载失败 / 超时 / 仍然污染导致 toBlob 抛 SecurityError）
 * 一律**优雅降级为无照片版式**，不让整张图导不出来。
 */
import type { Ticket } from '$lib/api/types';
import { drawShareCard, type PhotoSource } from './draw';
import { SHARE_H, SHARE_W, buildShareModel, shareFileName, type ShareModel } from './layout';

/** 高清：按 devicePixelRatio 放大绘制；上限 2 防止超出 WebView 画布尺寸限制 */
const MAX_SCALE = 2;
const PHOTO_TIMEOUT_MS = 8000;

export interface ShareRender {
	blob: Blob;
	model: ShareModel;
	fileName: string;
	/** false = 照片加载失败/跨域受限，已降级为无照片版式 */
	withPhoto: boolean;
}

function pixelScale(): number {
	const dpr = typeof window === 'undefined' ? 1 : (window.devicePixelRatio || 1);
	return Math.min(Math.max(dpr, 1), MAX_SCALE);
}

/** 加载票面照片；跨域必须匿名请求，失败/超时返回 null（不抛） */
export function loadPhoto(url: string, timeoutMs = PHOTO_TIMEOUT_MS): Promise<PhotoSource | null> {
	if (!url || typeof Image === 'undefined') return Promise.resolve(null);
	return new Promise((resolve) => {
		const img = new Image();
		let done = false;
		const finish = (value: PhotoSource | null) => {
			if (done) return;
			done = true;
			clearTimeout(timer);
			resolve(value);
		};
		const timer = setTimeout(() => finish(null), timeoutMs);
		// 关键：签名 URL 跨域 → 匿名 CORS，否则 canvas 被污染无法导出
		img.crossOrigin = 'anonymous';
		img.decoding = 'async';
		img.onload = () =>
			finish(
				img.naturalWidth > 0 && img.naturalHeight > 0
					? { image: img, width: img.naturalWidth, height: img.naturalHeight }
					: null
			);
		img.onerror = () => finish(null);
		img.src = url;
	});
}

function toBlob(canvas: HTMLCanvasElement): Promise<Blob> {
	return new Promise((resolve, reject) => {
		try {
			canvas.toBlob(
				(blob) => (blob ? resolve(blob) : reject(new Error('导出失败：画布为空'))),
				'image/png'
			);
		} catch (error) {
			// 画布被污染 → SecurityError（同步抛）
			reject(error instanceof Error ? error : new Error('导出失败'));
		}
	});
}

function paint(model: ShareModel, photo: PhotoSource | null): HTMLCanvasElement {
	const scale = pixelScale();
	const canvas = document.createElement('canvas');
	canvas.width = Math.round(SHARE_W * scale);
	canvas.height = Math.round(SHARE_H * scale);
	const ctx = canvas.getContext('2d');
	if (!ctx) throw new Error('当前环境不支持 canvas 绘制');
	ctx.scale(scale, scale);
	drawShareCard(ctx, model, { photo });
	return canvas;
}

/**
 * 渲染分享图。有照片先按带照片版式画；若因跨域污染导不出，
 * 自动回退无照片版式重画一次。
 */
export async function renderShareImage(ticket: Ticket): Promise<ShareRender> {
	const model = buildShareModel(ticket);
	const fileName = shareFileName(model);
	const photoUrl = ticket.attachments[0]?.url ?? '';
	const photo = await loadPhoto(photoUrl);

	if (photo) {
		try {
			const blob = await toBlob(paint(model, photo));
			return { blob, model, fileName, withPhoto: true };
		} catch {
			// 污染或导出失败 → 降级，不让整个导出失败
		}
	}
	const blob = await toBlob(paint(model, null));
	return { blob, model, fileName, withPhoto: false };
}

export type ShareOutcome = 'shared' | 'downloaded' | 'cancelled';

function download(blob: Blob, fileName: string): ShareOutcome {
	const url = URL.createObjectURL(blob);
	const a = document.createElement('a');
	a.href = url;
	a.download = fileName;
	a.rel = 'noopener';
	document.body.appendChild(a);
	a.click();
	a.remove();
	// 交给浏览器取走 blob 后再释放
	setTimeout(() => URL.revokeObjectURL(url), 10_000);
	return 'downloaded';
}

/**
 * 移动端（Capacitor WebView）优先 navigator.share 分享 File；
 * 不支持 / 分享失败 → 回退浏览器下载。用户主动取消不再回退。
 */
export async function shareOrDownload(blob: Blob, fileName: string): Promise<ShareOutcome> {
	const file =
		typeof File === 'undefined' ? null : new File([blob], fileName, { type: 'image/png' });
	const nav = typeof navigator === 'undefined' ? undefined : navigator;

	if (file && nav?.share && nav.canShare?.({ files: [file] })) {
		try {
			await nav.share({ files: [file], title: fileName });
			return 'shared';
		} catch (error) {
			if (error instanceof DOMException && error.name === 'AbortError') return 'cancelled';
			// 其他失败（无分享目标等）→ 回退下载
		}
	}
	return download(blob, fileName);
}

export { download as downloadImage };
