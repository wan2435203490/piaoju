/**
 * 票根分享图 —— 纯计算层（无 DOM、无 canvas，可在 node 下单测）。
 *
 * 设计空间：1080×1440 竖版（社媒 3:4）。所有尺寸 = design skill 的
 * token 阶梯 ×3（4px 网格 → 12、字号 12/14/16/20/28 → 36/42/48/60/84、
 * 票根卡圆角 16 → 48、色条 4/胶片条 14 → 12/42、打孔 14 → 42）。
 * 没有裸魔数：每个常量都在此处命名并注明来源。
 *
 * 分享图**固定亮色纸感版式**（design §5 的暗色只作用于 App UI，
 * 分享出去的图不该跟着系统主题变）。canvas 2D 不能引用 CSS 变量，
 * 因此 PAPER 是 app.css `:root`（Light）token 的镜像，改 token 需同步这里。
 */
import type { Ticket, TicketKind } from '$lib/api/types';
import { formatCents } from '$lib/utils/money';
import { KIND_META, fmtDateTime, stubMeta } from '$lib/components/ticket/kinds';

/* ============ 画布与版式常量（design token ×3） ============ */

export const SHARE_W = 1080;
export const SHARE_H = 1440;

/** 页面左右留白 16 ×3 */
export const PAGE_PAD = 48;
/** 票根卡圆角 16 ×3 */
export const CARD_RADIUS = 48;
/** 左缘票型色条 4 ×3；movie 胶片齿孔条 14 ×3 */
export const EDGE_W = 12;
export const EDGE_W_MOVIE = 42;
/** 胶片孔：半径 2.5 ×3，纵向节距 12 ×3 */
export const FILM_HOLE_R = 7.5;
export const FILM_HOLE_PITCH = 36;
/** 撕票线半圆打孔缺口 14 ×3（直径） */
export const PUNCH = 42;
/** 撕票线到卡片底的距离（脚注区高度） */
export const FOOT_H = 220;
/** 内容区左右内边距 12/16 ×3 */
export const CONTENT_PAD = 36;
export const CONTENT_PAD_RIGHT = 48;

/** 字号阶梯 ×3：12 辅助 / 14 正文 / 16 强调 / 20 标题 / 28 金额大数 */
export const FS_SMALL = 36;
export const FS_BODY = 42;
export const FS_STRONG = 48;
export const FS_TITLE = 60;
export const FS_AMOUNT = 84;

/** 行高：正文 1.5、标题 1.25（design §1） */
export const LH_TITLE = Math.round(FS_TITLE * 1.25); // 75
export const LH_BODY = Math.round(FS_BODY * 1.5); // 63

/** 不引 webfont（包体红线）：与 app.css --font-sans 完全一致 */
export const FONT_STACK = `system-ui, -apple-system, "PingFang SC", "Noto Sans SC", sans-serif`;

export const BRAND_TEXT = '拾光票局';

/** 分享图调色板：app.css `:root`（Light）token 镜像 + 票型色标（两模式同值） */
export interface Palette {
	bg: string;
	surface: string;
	ink: string;
	ink2: string;
	line: string;
	brand: string;
	kind: Record<TicketKind, string>;
}

export const PAPER: Palette = {
	bg: '#faf6ef',
	surface: '#ffffff',
	ink: '#292524',
	ink2: '#78716c',
	line: '#e7e0d3',
	brand: '#c2410c',
	kind: {
		movie: '#e11d48',
		show: '#9333ea',
		attraction: '#ea580c',
		train: '#16a34a',
		flight: '#2563eb',
		other: '#64748b'
	}
};

/* ============ 版式计算 ============ */

export interface Rect {
	x: number;
	y: number;
	w: number;
	h: number;
}

export interface ShareLayout {
	card: Rect;
	/** 顶部 16:9 图片区（有照片=照片，无照片=票型色块底纹） */
	hero: Rect;
	edgeW: number;
	/** 内容区（撕票线以上） */
	content: Rect;
	/** 撕票线 y */
	tearY: number;
	/** 脚注区（撕票线以下：票型标识 + 品牌小字） */
	foot: Rect;
}

/** 版式只随 kind 变化（movie 色条更宽 → 内容左移） */
export function layoutShare(kind: TicketKind): ShareLayout {
	const card: Rect = {
		x: PAGE_PAD,
		y: PAGE_PAD,
		w: SHARE_W - PAGE_PAD * 2,
		h: SHARE_H - PAGE_PAD * 2
	};
	const edgeW = kind === 'movie' ? EDGE_W_MOVIE : EDGE_W;
	// 16:9 裁切（design §2）
	const hero: Rect = { x: card.x, y: card.y, w: card.w, h: Math.round((card.w * 9) / 16) };
	const tearY = card.y + card.h - FOOT_H;
	const contentX = card.x + edgeW + CONTENT_PAD;
	const contentTop = hero.y + hero.h + CONTENT_PAD + 18; // 12+6 网格内的呼吸位
	const content: Rect = {
		x: contentX,
		y: contentTop,
		w: card.x + card.w - CONTENT_PAD_RIGHT - contentX,
		h: tearY - contentTop
	};
	const foot: Rect = { x: contentX, y: tearY, w: content.w, h: FOOT_H };
	return { card, hero, edgeW, content, tearY, foot };
}

/** 胶片齿孔条上所有孔的圆心 y（在 [y0, y1] 内按节距均匀铺开） */
export function filmHoleCenters(y0: number, y1: number, pitch = FILM_HOLE_PITCH): number[] {
	if (!(pitch > 0) || y1 <= y0) return [];
	const count = Math.floor((y1 - y0) / pitch);
	return Array.from({ length: count }, (_, i) => y0 + pitch * (i + 0.5));
}

/** 16:9 cover 裁切：返回源图上要取的矩形（居中裁切，等比不变形） */
export function coverRect(srcW: number, srcH: number, dstW: number, dstH: number): Rect {
	if (!(srcW > 0) || !(srcH > 0) || !(dstW > 0) || !(dstH > 0)) {
		return { x: 0, y: 0, w: Math.max(srcW, 0), h: Math.max(srcH, 0) };
	}
	const srcRatio = srcW / srcH;
	const dstRatio = dstW / dstH;
	if (srcRatio > dstRatio) {
		// 源图更宽 → 裁左右
		const w = srcH * dstRatio;
		return { x: (srcW - w) / 2, y: 0, w, h: srcH };
	}
	// 源图更高 → 裁上下
	const h = srcW / dstRatio;
	return { x: 0, y: (srcH - h) / 2, w: srcW, h };
}

/* ============ 中文折行（canvas 无自动换行，自己按字符宽度折） ============ */

export type Measure = (text: string) => number;

/**
 * 切词：CJK/emoji 逐字可断，ASCII 连续串（单词、数字、"IMAX"、"G102"）不拆，
 * 空白作为可丢弃的断点。
 */
export function tokenize(text: string): string[] {
	const tokens: string[] = [];
	let word = '';
	for (const ch of text) {
		if (/\s/.test(ch)) {
			if (word) tokens.push(word);
			word = '';
			tokens.push(' ');
			continue;
		}
		// ASCII 可见字符（含标点/数字）聚成一个不可拆的词
		if (ch.length === 1 && ch >= '!' && ch <= '~') {
			word += ch;
			continue;
		}
		if (word) tokens.push(word);
		word = '';
		tokens.push(ch);
	}
	if (word) tokens.push(word);
	return tokens;
}

/** 超宽单串（长英文单词）按字符硬断 */
function hardBreak(measure: Measure, token: string, maxWidth: number): string[] {
	const out: string[] = [];
	let line = '';
	for (const ch of token) {
		if (line && measure(line + ch) > maxWidth) {
			out.push(line);
			line = ch;
		} else {
			line += ch;
		}
	}
	if (line) out.push(line);
	return out;
}

/** 末行省略：一直砍到能塞下 "…" 为止 */
export function ellipsize(measure: Measure, text: string, maxWidth: number): string {
	if (measure(text) <= maxWidth) return text;
	const chars = [...text];
	while (chars.length > 0) {
		chars.pop();
		const candidate = `${chars.join('').trimEnd()}…`;
		if (measure(candidate) <= maxWidth) return candidate;
	}
	return '…';
}

/**
 * 折行：返回不超过 maxLines 行、每行宽度 ≤ maxWidth 的文本行。
 * 超出 maxLines 时末行以 "…" 收尾。`\n` 为强制换行。
 */
export function wrapText(
	measure: Measure,
	text: string,
	maxWidth: number,
	maxLines = Number.POSITIVE_INFINITY
): string[] {
	if (!text || maxWidth <= 0 || maxLines < 1) return [];
	const lines: string[] = [];
	let overflow = false;

	for (const paragraph of text.split('\n')) {
		if (overflow) break;
		let line = '';
		for (const token of tokenize(paragraph)) {
			if (token === ' ') {
				if (line) line += ' ';
				continue;
			}
			if (measure(line + token) <= maxWidth) {
				line += token;
				continue;
			}
			// 当前行放不下：先落行
			if (line.trim()) {
				lines.push(line.trimEnd());
				line = '';
				if (lines.length >= maxLines) {
					overflow = true;
					break;
				}
			}
			// 单 token 仍超宽 → 硬断
			if (measure(token) > maxWidth) {
				const pieces = hardBreak(measure, token, maxWidth);
				const last = pieces.pop() ?? '';
				for (const piece of pieces) {
					lines.push(piece);
					if (lines.length >= maxLines) {
						overflow = true;
						break;
					}
				}
				if (overflow) break;
				line = last;
			} else {
				line = token;
			}
		}
		if (overflow) break;
		if (line.trim()) lines.push(line.trimEnd());
		if (lines.length >= maxLines) {
			overflow = true;
			break;
		}
	}

	if (lines.length > maxLines) lines.length = maxLines;
	// 有内容被截断 → 末行省略号
	if (overflow) {
		const rendered = lines.join('');
		const source = text.replace(/\s|\n/g, '');
		if (rendered.replace(/\s/g, '').length < source.length) {
			const last = lines[lines.length - 1];
			if (last !== undefined) lines[lines.length - 1] = ellipsize(measure, `${last}…`, maxWidth);
		}
	}
	return lines;
}

/* ============ 数据模型（Ticket → 分享图文案） ============ */

export interface ShareModel {
	kind: TicketKind;
	kindLabel: string;
	kindEmoji: string;
	kindColor: string;
	title: string;
	/** 场馆 + 票型专属信息（复用票根卡第二行逻辑） */
	venue: string;
	/** 本地时区展示 */
	time: string;
	seat: string;
	rating: number;
	memo: string;
	/** "¥45.00"（整数分渲染层才除 100 —— conventions §1；分享图不带 ± 号） */
	amount: string;
}

export function buildShareModel(ticket: Ticket): ShareModel {
	const meta = KIND_META[ticket.kind];
	return {
		kind: ticket.kind,
		kindLabel: meta.label,
		kindEmoji: meta.emoji,
		kindColor: PAPER.kind[ticket.kind],
		title: ticket.title,
		venue: stubMeta(ticket),
		time: fmtDateTime(ticket.eventTime),
		seat: ticket.seat,
		rating: Math.max(0, Math.min(5, Math.round(ticket.rating))),
		memo: ticket.memo,
		amount: `¥${formatCents(Math.abs(ticket.transaction.amountCents))}`
	};
}

/** 导出文件名：票名 + 日期，去掉文件系统敏感字符 */
export function shareFileName(model: ShareModel): string {
	const safe = model.title.replace(/[\\/:*?"<>|\s]+/g, '-').slice(0, 40) || 'ticket';
	const day = model.time.slice(0, 10) || 'stub';
	return `${safe}-${day}.png`;
}
