/**
 * 票根分享图 —— 绘制层。纯函数 drawShareCard(ctx, model, opts)：
 * 只依赖一个最小 2D context 接口（Ctx2D），因此 node 环境可用假 ctx 单测。
 *
 * 视觉严格照 design skill §2 的票根语言：
 * - 左缘票型色条；movie 换成胶片齿孔条（色条 + 挖出 --bg 色圆孔）
 * - 撕票线：虚线 + 左右两个半圆打孔缺口
 * - 顶部 16:9 照片区；无照片 → 票型图标 + 色块底纹（不留空白灰框）
 * 固定亮色纸感（PAPER），不跟随系统暗色。
 */
import {
	BRAND_TEXT,
	CARD_RADIUS,
	FILM_HOLE_R,
	FONT_STACK,
	FS_AMOUNT,
	FS_BODY,
	FS_SMALL,
	FS_STRONG,
	FS_TITLE,
	LH_BODY,
	LH_TITLE,
	PAPER,
	PUNCH,
	SHARE_H,
	SHARE_W,
	coverRect,
	ellipsize,
	filmHoleCenters,
	layoutShare,
	wrapText,
	type Palette,
	type ShareModel
} from './layout';

/** CanvasRenderingContext2D 的结构子集（真 ctx 可直接赋值；测试可造假） */
export interface Ctx2D {
	// 与 CanvasRenderingContext2D 同型（本模块只赋字符串色值）
	fillStyle: string | CanvasGradient | CanvasPattern;
	strokeStyle: string | CanvasGradient | CanvasPattern;
	lineWidth: number;
	font: string;
	textAlign: CanvasTextAlign;
	textBaseline: CanvasTextBaseline;
	globalAlpha: number;
	save(): void;
	restore(): void;
	beginPath(): void;
	closePath(): void;
	moveTo(x: number, y: number): void;
	lineTo(x: number, y: number): void;
	arc(x: number, y: number, r: number, start: number, end: number): void;
	arcTo(x1: number, y1: number, x2: number, y2: number, r: number): void;
	rect(x: number, y: number, w: number, h: number): void;
	fill(): void;
	stroke(): void;
	clip(): void;
	fillRect(x: number, y: number, w: number, h: number): void;
	fillText(text: string, x: number, y: number): void;
	measureText(text: string): { width: number };
	setLineDash(segments: number[]): void;
	drawImage(
		image: CanvasImageSource,
		sx: number,
		sy: number,
		sw: number,
		sh: number,
		dx: number,
		dy: number,
		dw: number,
		dh: number
	): void;
}

/** drawImage 的源（真实为 HTMLImageElement；测试传 {width,height} 即可） */
export interface PhotoSource {
	image: CanvasImageSource;
	width: number;
	height: number;
}

export interface DrawOptions {
	palette?: Palette;
	/** 加载成功且未污染的票面照片；缺省 → 走无照片版式 */
	photo?: PhotoSource | null;
}

const TAU = Math.PI * 2;
const STAR_FULL = '★';
const STAR_EMPTY = '☆';

const font = (size: number, weight: 400 | 600 | 700 = 400): string =>
	`${weight} ${size}px ${FONT_STACK}`;

function roundRectPath(ctx: Ctx2D, x: number, y: number, w: number, h: number, r: number): void {
	const radius = Math.min(r, w / 2, h / 2);
	ctx.beginPath();
	ctx.moveTo(x + radius, y);
	ctx.arcTo(x + w, y, x + w, y + h, radius);
	ctx.arcTo(x + w, y + h, x, y + h, radius);
	ctx.arcTo(x, y + h, x, y, radius);
	ctx.arcTo(x, y, x + w, y, radius);
	ctx.closePath();
}

function circle(ctx: Ctx2D, x: number, y: number, r: number, color: string): void {
	ctx.fillStyle = color;
	ctx.beginPath();
	ctx.arc(x, y, r, 0, TAU);
	ctx.fill();
}

/** 色块底纹 = 票型色 14% 透明度（同 CSS 的 color-mix 14%） */
function tintRect(ctx: Ctx2D, x: number, y: number, w: number, h: number, color: string): void {
	ctx.save();
	ctx.globalAlpha = 0.14;
	ctx.fillStyle = color;
	ctx.fillRect(x, y, w, h);
	ctx.restore();
}

/**
 * 绘制一整张分享图（设计坐标 1080×1440；DPR 缩放由调用方 ctx.scale 处理）。
 */
export function drawShareCard(ctx: Ctx2D, model: ShareModel, opts: DrawOptions = {}): void {
	const p = opts.palette ?? PAPER;
	const photo = opts.photo ?? null;
	const kindColor = p.kind[model.kind] ?? model.kindColor;
	const { card, hero, edgeW, content, tearY, foot } = layoutShare(model.kind);
	const contentRight = content.x + content.w;

	ctx.save();
	ctx.textBaseline = 'alphabetic';
	ctx.textAlign = 'left';
	ctx.setLineDash([]);

	/* ---- 纸感底 ---- */
	ctx.fillStyle = p.bg;
	ctx.fillRect(0, 0, SHARE_W, SHARE_H);

	/* ---- 票根卡 ---- */
	roundRectPath(ctx, card.x, card.y, card.w, card.h, CARD_RADIUS);
	ctx.fillStyle = p.surface;
	ctx.fill();

	/* ---- 卡内（照片 / 底纹 / 左缘色条）裁进圆角 ---- */
	ctx.save();
	roundRectPath(ctx, card.x, card.y, card.w, card.h, CARD_RADIUS);
	ctx.clip();

	if (photo) {
		const src = coverRect(photo.width, photo.height, hero.w, hero.h);
		ctx.drawImage(photo.image, src.x, src.y, src.w, src.h, hero.x, hero.y, hero.w, hero.h);
	} else {
		// 无照片：票型图标 + 色块底纹（design §2：不留空白灰框）
		tintRect(ctx, hero.x, hero.y, hero.w, hero.h, kindColor);
		ctx.textAlign = 'center';
		ctx.fillStyle = kindColor;
		ctx.font = font(FS_AMOUNT * 1.6);
		ctx.fillText(model.kindEmoji, hero.x + hero.w / 2, hero.y + hero.h / 2 + FS_AMOUNT * 0.5);
		ctx.font = font(FS_BODY, 600);
		ctx.fillText(model.kindLabel, hero.x + hero.w / 2, hero.y + hero.h / 2 + FS_AMOUNT * 1.5);
		ctx.textAlign = 'left';
	}

	// 左缘票型色条（贯穿全高，压在照片之上）
	ctx.fillStyle = kindColor;
	ctx.fillRect(card.x, card.y, edgeW, card.h);
	if (model.kind === 'movie') {
		// movie 专属：胶片齿孔条 —— 在色条上挖 --bg 色圆孔
		for (const cy of filmHoleCenters(card.y, card.y + card.h)) {
			circle(ctx, card.x + edgeW / 2, cy, FILM_HOLE_R, p.bg);
		}
	}
	ctx.restore();

	/* ---- 正文 ---- */
	const measureAt = (size: number, weight: 400 | 600 | 700 = 400) => {
		ctx.font = font(size, weight);
		return (text: string) => ctx.measureText(text).width;
	};

	let y = content.y + FS_TITLE;

	// 票名（最多 2 行，折行 + 省略）
	ctx.font = font(FS_TITLE, 700);
	ctx.fillStyle = p.ink;
	for (const line of wrapText(measureAt(FS_TITLE, 700), model.title, content.w, 2)) {
		ctx.font = font(FS_TITLE, 700);
		ctx.fillText(line, content.x, y);
		y += LH_TITLE;
	}
	y += 12;

	// 场馆 / 票型专属信息（1 行）
	if (model.venue) {
		ctx.font = font(FS_BODY, 400);
		ctx.fillStyle = p.ink2;
		ctx.fillText(ellipsize(measureAt(FS_BODY), model.venue, content.w), content.x, y);
		y += LH_BODY;
	}

	// 时间 / 座位
	ctx.fillStyle = p.ink;
	ctx.font = font(FS_BODY, 400);
	if (model.time) {
		ctx.fillText(model.time, content.x, y);
		y += LH_BODY;
	}
	if (model.seat) {
		ctx.fillText(ellipsize(measureAt(FS_BODY), model.seat, content.w), content.x, y);
		y += LH_BODY;
	}

	// 评分 + 金额同排，贴着撕票线上方（下对齐，避免长标题挤压）
	const amountTop = tearY - 132;

	// 感想（可选，1 行）：只有排得下才画，绝不压到金额排
	if (model.memo && y <= amountTop - LH_BODY) {
		ctx.font = font(FS_SMALL, 400);
		ctx.fillStyle = p.ink2;
		ctx.fillText(
			ellipsize(measureAt(FS_SMALL), `“${model.memo}”`, content.w),
			content.x,
			Math.max(y, amountTop - LH_BODY)
		);
	}

	// 评分星（brand 实星 + line 空星）
	ctx.font = font(FS_STRONG, 400);
	if (model.rating > 0) {
		const step = FS_STRONG * 1.15;
		for (let i = 0; i < 5; i += 1) {
			ctx.fillStyle = i < model.rating ? p.brand : p.line;
			ctx.fillText(i < model.rating ? STAR_FULL : STAR_EMPTY, content.x + step * i, amountTop + 60);
		}
	}

	// 金额大数（右对齐；分享图不带方向符号）
	ctx.font = font(FS_AMOUNT, 700);
	ctx.fillStyle = p.brand;
	ctx.textAlign = 'right';
	ctx.fillText(model.amount, contentRight, amountTop + FS_AMOUNT);
	ctx.textAlign = 'left';

	/* ---- 撕票线：虚线 + 两侧半圆打孔缺口 ---- */
	ctx.save();
	ctx.setLineDash([12, 12]);
	ctx.strokeStyle = p.line;
	ctx.lineWidth = 3;
	ctx.beginPath();
	ctx.moveTo(card.x + PUNCH, tearY);
	ctx.lineTo(card.x + card.w - PUNCH, tearY);
	ctx.stroke();
	ctx.restore();
	// 缺口：卡片左右边缘各挖一个纸底色半圆
	circle(ctx, card.x, tearY, PUNCH / 2, p.bg);
	circle(ctx, card.x + card.w, tearY, PUNCH / 2, p.bg);

	/* ---- 脚注：票型标识 + 品牌小字 ---- */
	const footCenter = foot.y + foot.h / 2;
	const badgeLabel = `${model.kindEmoji} ${model.kindLabel}`;
	ctx.font = font(FS_SMALL, 600);
	const badgeW = ctx.measureText(badgeLabel).width + 48;
	const badgeH = 72;
	ctx.save();
	ctx.globalAlpha = 0.14;
	roundRectPath(ctx, content.x, footCenter - badgeH / 2, badgeW, badgeH, badgeH / 2);
	ctx.fillStyle = kindColor;
	ctx.fill();
	ctx.restore();
	ctx.fillStyle = p.ink;
	ctx.font = font(FS_SMALL, 600);
	ctx.fillText(badgeLabel, content.x + 24, footCenter + FS_SMALL / 3);

	ctx.textAlign = 'right';
	ctx.fillStyle = p.ink2;
	ctx.font = font(FS_SMALL, 400);
	ctx.fillText(BRAND_TEXT, contentRight, footCenter + FS_SMALL / 3);
	ctx.textAlign = 'left';

	ctx.restore();
}
