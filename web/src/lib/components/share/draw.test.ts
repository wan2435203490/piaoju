/**
 * drawShareCard 单测：node 无 canvas → 用假的 2D context 录调用序列，
 * 断言票根视觉语言（纸底、卡片、色条、胶片孔、撕票线 + 半圆缺口、
 * 16:9 照片 / 无照片降级底纹）与全部文案都被画出。
 */
import { describe, expect, it } from 'vitest';
import { fixtureTickets } from '$lib/api/fixtures';
import type { Ticket, TicketKind } from '$lib/api/types';
import { drawShareCard, type Ctx2D, type PhotoSource } from './draw';
import {
	FILM_HOLE_R,
	PAPER,
	PUNCH,
	SHARE_H,
	SHARE_W,
	buildShareModel,
	layoutShare
} from './layout';

interface Call {
	op: string;
	args: number[];
	text?: string;
	style: string;
	/** fillText 时的字号（px），用于断言文本宽度 */
	size?: number;
}

const fontSize = (font: string): number =>
	Number.parseFloat(/(\d+(?:\.\d+)?)px/.exec(font)?.[1] ?? '10');

/** 与 fake measureText 同口径：CJK/emoji 1em，ASCII 0.5em */
const textWidth = (text: string, size: number): number =>
	[...text].reduce((w, ch) => w + (ch.charCodeAt(0) < 128 ? size * 0.5 : size), 0);

interface FakeCtx extends Ctx2D {
	// 本模块只用字符串色值 → 收窄，便于断言
	fillStyle: string;
	strokeStyle: string;
	calls: Call[];
	texts: string[];
	/** 每次 setLineDash 的参数 */
	dashes: number[][];
	depth: number;
	maxDepth: number;
}

/** 假 ctx：measureText 用 CJK 1em / ASCII 0.5em 近似 */
function fakeCtx(): FakeCtx {
	const calls: Call[] = [];
	const texts: string[] = [];
	const dashes: number[][] = [];
	let depth = 0;
	let maxDepth = 0;

	const ctx: FakeCtx = {
		fillStyle: '#000',
		strokeStyle: '#000',
		lineWidth: 1,
		font: '10px sans-serif',
		textAlign: 'left',
		textBaseline: 'alphabetic',
		globalAlpha: 1,
		calls,
		texts,
		dashes,
		get depth() {
			return depth;
		},
		set depth(v: number) {
			depth = v;
		},
		get maxDepth() {
			return maxDepth;
		},
		set maxDepth(v: number) {
			maxDepth = v;
		},
		save() {
			depth += 1;
			maxDepth = Math.max(maxDepth, depth);
			calls.push({ op: 'save', args: [], style: ctx.fillStyle });
		},
		restore() {
			depth -= 1;
			calls.push({ op: 'restore', args: [], style: ctx.fillStyle });
		},
		beginPath: () => calls.push({ op: 'beginPath', args: [], style: ctx.fillStyle }),
		closePath: () => calls.push({ op: 'closePath', args: [], style: ctx.fillStyle }),
		moveTo: (x, y) => calls.push({ op: 'moveTo', args: [x, y], style: ctx.strokeStyle }),
		lineTo: (x, y) => calls.push({ op: 'lineTo', args: [x, y], style: ctx.strokeStyle }),
		arc: (x, y, r) => calls.push({ op: 'arc', args: [x, y, r], style: ctx.fillStyle }),
		arcTo: (x1, y1, x2, y2, r) =>
			calls.push({ op: 'arcTo', args: [x1, y1, x2, y2, r], style: ctx.fillStyle }),
		rect: (x, y, w, h) => calls.push({ op: 'rect', args: [x, y, w, h], style: ctx.fillStyle }),
		fill: () => calls.push({ op: 'fill', args: [], style: ctx.fillStyle }),
		stroke: () => calls.push({ op: 'stroke', args: [], style: ctx.strokeStyle }),
		clip: () => calls.push({ op: 'clip', args: [], style: ctx.fillStyle }),
		fillRect: (x, y, w, h) =>
			calls.push({ op: 'fillRect', args: [x, y, w, h], style: ctx.fillStyle }),
		fillText(text, x, y) {
			texts.push(text);
			calls.push({
				op: 'fillText',
				args: [x, y],
				text,
				style: ctx.fillStyle,
				size: fontSize(ctx.font)
			});
		},
		measureText: (text: string) => ({ width: textWidth(text, fontSize(ctx.font)) }),
		setLineDash: (segments: number[]) => {
			dashes.push([...segments]);
		},
		drawImage: (_img, sx, sy, sw, sh, dx, dy, dw, dh) =>
			calls.push({ op: 'drawImage', args: [sx, sy, sw, sh, dx, dy, dw, dh], style: '' })
	};
	return ctx;
}

function fixtureOf(kind: TicketKind): Ticket {
	const t = fixtureTickets.find((x) => x.kind === kind);
	if (!t) throw new Error(`fixtures 缺少 ${kind}`);
	return t;
}

const photo: PhotoSource = { image: {} as CanvasImageSource, width: 2000, height: 1000 };

const draw = (ticket: Ticket, withPhoto = false): FakeCtx => {
	const ctx = fakeCtx();
	drawShareCard(ctx, buildShareModel(ticket), { photo: withPhoto ? photo : null });
	return ctx;
};

const has = (ctx: FakeCtx, op: string, pred: (c: Call) => boolean): boolean =>
	ctx.calls.some((c) => c.op === op && pred(c));

describe('drawShareCard 纸感底 + 票根卡', () => {
	it('先铺满 1080×1440 纸底色，再画白色卡片', () => {
		const ctx = draw(fixtureOf('show'));
		const first = ctx.calls.find((c) => c.op === 'fillRect');
		expect(first?.args).toEqual([0, 0, SHARE_W, SHARE_H]);
		expect(first?.style).toBe(PAPER.bg);
		// 卡片用圆角路径（arcTo ×4）+ surface 填充
		expect(has(ctx, 'fill', (c) => c.style === PAPER.surface)).toBe(true);
		expect(ctx.calls.filter((c) => c.op === 'arcTo').length).toBeGreaterThanOrEqual(4);
	});

	it('save/restore 严格配对（不泄漏画布状态）', () => {
		const ctx = draw(fixtureOf('flight'), true);
		expect(ctx.depth).toBe(0);
		expect(ctx.maxDepth).toBeGreaterThan(0);
	});
});

describe('票型色条与 movie 胶片齿孔条（design §2）', () => {
	it('非 movie：左缘 12px 纯色条，无胶片孔', () => {
		const ctx = draw(fixtureOf('train'));
		const { card } = layoutShare('train');
		expect(
			has(
				ctx,
				'fillRect',
				(c) =>
					c.style === PAPER.kind.train &&
					c.args[0] === card.x &&
					c.args[2] === 12 &&
					c.args[3] === card.h
			)
		).toBe(true);
		expect(has(ctx, 'arc', (c) => c.args[2] === FILM_HOLE_R)).toBe(false);
	});

	it('movie：42px 色条 + 一列纸底色齿孔（孔心在条中线上）', () => {
		const ctx = draw(fixtureOf('movie'));
		const { card, edgeW } = layoutShare('movie');
		expect(has(ctx, 'fillRect', (c) => c.style === PAPER.kind.movie && c.args[2] === 42)).toBe(true);
		const holes = ctx.calls.filter(
			(c) => c.op === 'arc' && c.args[2] === FILM_HOLE_R && c.style === PAPER.bg
		);
		expect(holes.length).toBeGreaterThan(20);
		for (const hole of holes) expect(hole.args[0]).toBe(card.x + edgeW / 2);
	});
});

describe('撕票线', () => {
	it('虚线横穿卡片 + 左右两个纸底色半圆缺口', () => {
		const ctx = draw(fixtureOf('attraction'));
		const { card, tearY } = layoutShare('attraction');
		expect(ctx.dashes.some((d) => d.length === 2 && d[0]! > 0)).toBe(true);
		expect(has(ctx, 'moveTo', (c) => c.args[1] === tearY)).toBe(true);
		expect(has(ctx, 'stroke', (c) => c.style === PAPER.line)).toBe(true);
		const punches = ctx.calls.filter(
			(c) => c.op === 'arc' && c.args[2] === PUNCH / 2 && c.args[1] === tearY
		);
		expect(punches.map((c) => c.args[0])).toEqual([card.x, card.x + card.w]);
		expect(punches.every((c) => c.style === PAPER.bg)).toBe(true);
	});
});

describe('图片区', () => {
	it('有照片：16:9 cover 裁切绘入顶部图片区', () => {
		const ctx = draw(fixtureOf('show'), true);
		const { hero } = layoutShare('show');
		const call = ctx.calls.find((c) => c.op === 'drawImage');
		expect(call).toBeDefined();
		const [, , sw, sh, dx, dy, dw, dh] = call!.args as [
			number,
			number,
			number,
			number,
			number,
			number,
			number,
			number
		];
		// 源 2000×1000 更宽 → 裁左右，取满高
		expect(sh).toBe(1000);
		expect(sw).toBeCloseTo((1000 * hero.w) / hero.h);
		expect([dx, dy, dw, dh]).toEqual([hero.x, hero.y, hero.w, hero.h]);
	});

	it('无照片：降级为票型色块底纹 + 图标 + 票型名，绝不留空白且不调 drawImage', () => {
		const ctx = draw(fixtureOf('show'), false);
		const { hero } = layoutShare('show');
		expect(has(ctx, 'drawImage', () => true)).toBe(false);
		expect(
			has(
				ctx,
				'fillRect',
				(c) =>
					c.style === PAPER.kind.show &&
					c.args[0] === hero.x &&
					c.args[1] === hero.y &&
					c.args[3] === hero.h
			)
		).toBe(true);
		expect(ctx.texts).toContain('🎭');
	});
});

describe('文案内容', () => {
	it('票名/场馆/时间/座位/金额/品牌/票型标识齐备', () => {
		const ticket = fixtureOf('movie');
		const ctx = draw(ticket, true);
		const model = buildShareModel(ticket);
		const all = ctx.texts.join('|');
		expect(all).toContain(model.title);
		expect(all).toContain(model.time);
		expect(all).toContain(model.seat);
		expect(ctx.texts).toContain(model.amount);
		expect(ctx.texts).toContain('拾光票局');
		expect(ctx.texts).toContain(`${model.kindEmoji} ${model.kindLabel}`);
	});

	it('评分画 5 颗星：实星用 brand 色，空星用 line 色', () => {
		const ticket = { ...fixtureOf('movie'), rating: 4 };
		const ctx = draw(ticket);
		const stars = ctx.calls.filter((c) => c.op === 'fillText' && /[★☆]/.test(c.text ?? ''));
		expect(stars).toHaveLength(5);
		expect(stars.filter((c) => c.text === '★' && c.style === PAPER.brand)).toHaveLength(4);
		expect(stars.filter((c) => c.text === '☆' && c.style === PAPER.line)).toHaveLength(1);
	});

	it('未评分（rating=0）不画星', () => {
		const ctx = draw({ ...fixtureOf('show'), rating: 0 });
		expect(ctx.calls.some((c) => c.op === 'fillText' && /[★☆]/.test(c.text ?? ''))).toBe(false);
	});

	it('超长标题折行（最多 2 行 + 省略号），每行不超内容区宽度', () => {
		const ticket = { ...fixtureOf('show'), title: '超长票名'.repeat(20) };
		const ctx = draw(ticket);
		const { content } = layoutShare('show');
		const titleLines = ctx.calls.filter((c) => c.op === 'fillText' && c.size === 60);
		expect(titleLines).toHaveLength(2);
		expect(titleLines[1]?.text?.endsWith('…')).toBe(true);
		for (const line of titleLines) {
			expect(textWidth(line.text ?? '', 60)).toBeLessThanOrEqual(content.w);
		}
	});

	it('左对齐的正文文本一律不超出内容区右边界', () => {
		const ticket = { ...fixtureOf('flight'), memo: '很久没坐过这么颠的航班了'.repeat(6) };
		const ctx = draw(ticket);
		const { content } = layoutShare('flight');
		const leftAligned = ctx.calls.filter(
			(c) => c.op === 'fillText' && c.args[0] === content.x && c.size !== undefined
		);
		expect(leftAligned.length).toBeGreaterThan(0);
		for (const line of leftAligned) {
			expect(line.args[0]! + textWidth(line.text ?? '', line.size!)).toBeLessThanOrEqual(
				content.x + content.w
			);
		}
	});

	it('所有文字都落在卡片内（不出血）', () => {
		const ctx = draw(fixtureOf('flight'), true);
		const { card } = layoutShare('flight');
		for (const call of ctx.calls.filter((c) => c.op === 'fillText')) {
			expect(call.args[0]).toBeGreaterThanOrEqual(card.x);
			expect(call.args[0]).toBeLessThanOrEqual(card.x + card.w);
			expect(call.args[1]).toBeGreaterThan(card.y);
			expect(call.args[1]).toBeLessThan(card.y + card.h);
		}
	});

	it('五种票型 + 有无照片全组合都能画完（不抛）', () => {
		for (const ticket of fixtureTickets) {
			for (const withPhoto of [true, false]) {
				expect(() => draw(ticket, withPhoto)).not.toThrow();
			}
		}
	});
});
