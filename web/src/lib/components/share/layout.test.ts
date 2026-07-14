/**
 * 分享图纯计算层单测：折行（中文/英文/硬断/省略）、cover 裁切、
 * 版式尺寸、胶片孔、Ticket → ShareModel。
 */
import { describe, expect, it } from 'vitest';
import { fixtureTickets } from '$lib/api/fixtures';
import type { Ticket, TicketKind } from '$lib/api/types';
import {
	EDGE_W,
	EDGE_W_MOVIE,
	FILM_HOLE_PITCH,
	SHARE_H,
	SHARE_W,
	buildShareModel,
	coverRect,
	ellipsize,
	filmHoleCenters,
	layoutShare,
	shareFileName,
	tokenize,
	wrapText,
	type Measure
} from './layout';

/** 假 measure：CJK/emoji 宽 1em，ASCII 宽 0.5em（近似真实字形，可精确断言） */
function measureAt(size: number): Measure {
	return (text: string) =>
		[...text].reduce((w, ch) => w + (ch.charCodeAt(0) < 128 ? size * 0.5 : size), 0);
}

const m20 = measureAt(20); // 中文 20px/字，ASCII 10px/字

function fixtureOf(kind: TicketKind): Ticket {
	const t = fixtureTickets.find((x) => x.kind === kind);
	if (!t) throw new Error(`fixtures 缺少 ${kind}`);
	return t;
}

describe('tokenize', () => {
	it('CJK 逐字可断，ASCII 连续串不拆，空白单独成 token', () => {
		expect(tokenize('沙丘 IMAX-3D 场')).toEqual(['沙', '丘', ' ', 'IMAX-3D', ' ', '场']);
	});

	it('emoji 作为单个 token（按 code point 迭代，不切断代理对）', () => {
		expect(tokenize('🎬电影')).toEqual(['🎬', '电', '影']);
	});
});

describe('wrapText 折行', () => {
	it('中文按字符宽度折行（每行 5 字 = 100px）', () => {
		const lines = wrapText(m20, '一二三四五六七八九十十一', 100);
		expect(lines).toEqual(['一二三四五', '六七八九十', '十一']);
	});

	it('英文单词不被拆断，空格作为断点', () => {
		// maxWidth 100px = 10 个 ASCII 字符
		const lines = wrapText(m20, 'hello brave new world', 100);
		expect(lines).toEqual(['hello', 'brave new', 'world']);
	});

	it('超宽单词硬断', () => {
		const lines = wrapText(m20, 'supercalifragilistic', 100);
		expect(lines).toEqual(['supercalif', 'ragilistic']);
	});

	it('超出 maxLines 时末行以省略号收尾', () => {
		const lines = wrapText(m20, '一二三四五六七八九十十一十二', 100, 2);
		expect(lines).toHaveLength(2);
		expect(lines[0]).toBe('一二三四五');
		expect(lines[1]?.endsWith('…')).toBe(true);
		// 每行仍不超宽
		for (const line of lines) expect(m20(line)).toBeLessThanOrEqual(100);
	});

	it('未截断时不加省略号', () => {
		expect(wrapText(m20, '一二三四五', 100, 2)).toEqual(['一二三四五']);
	});

	it('\\n 强制换行', () => {
		expect(wrapText(m20, '一二\n三四', 100)).toEqual(['一二', '三四']);
	});

	it('空串 / 非法宽度 → 空数组（不抛）', () => {
		expect(wrapText(m20, '', 100)).toEqual([]);
		expect(wrapText(m20, '一二三', 0)).toEqual([]);
		expect(wrapText(m20, '一二三', 100, 0)).toEqual([]);
	});

	it('任何输入下每行宽度都 ≤ maxWidth', () => {
		const lines = wrapText(m20, '沙丘 3 IMAX 首映场 with friends 超长标题继续继续继续', 160);
		expect(lines.length).toBeGreaterThan(1);
		for (const line of lines) expect(m20(line)).toBeLessThanOrEqual(160);
	});
});

describe('ellipsize', () => {
	it('放得下原样返回', () => {
		expect(ellipsize(m20, '一二三', 100)).toBe('一二三');
	});

	it('放不下时截断并补 …，且不超宽', () => {
		const out = ellipsize(m20, '一二三四五六七八', 100);
		expect(out.endsWith('…')).toBe(true);
		expect(m20(out)).toBeLessThanOrEqual(100);
	});
});

describe('coverRect 16:9 居中裁切', () => {
	it('源图更宽 → 裁左右，高度取满', () => {
		const r = coverRect(2000, 1000, 16, 9); // 源 2:1 比 16:9 更宽
		expect(r.h).toBe(1000);
		expect(r.w).toBeCloseTo((1000 * 16) / 9);
		expect(r.x).toBeCloseTo((2000 - (1000 * 16) / 9) / 2);
		expect(r.y).toBe(0);
	});

	it('源图更高 → 裁上下，宽度取满', () => {
		const r = coverRect(900, 1600, 16, 9);
		expect(r.w).toBe(900);
		expect(r.h).toBeCloseTo((900 * 9) / 16);
		expect(r.x).toBe(0);
		expect(r.y).toBeCloseTo((1600 - (900 * 9) / 16) / 2);
	});

	it('比例相同 → 全图', () => {
		expect(coverRect(1600, 900, 16, 9)).toEqual({ x: 0, y: 0, w: 1600, h: 900 });
	});

	it('非法尺寸不抛', () => {
		expect(coverRect(0, 0, 16, 9)).toEqual({ x: 0, y: 0, w: 0, h: 0 });
	});
});

describe('layoutShare 版式', () => {
	it('卡片在 1080×1440 内左右各留 48 页边距', () => {
		const { card } = layoutShare('other');
		expect(card.x).toBe(48);
		expect(card.w).toBe(SHARE_W - 96);
		expect(card.y + card.h).toBe(SHARE_H - 48);
	});

	it('顶部图片区严格 16:9', () => {
		const { hero } = layoutShare('other');
		expect(hero.h).toBe(Math.round((hero.w * 9) / 16));
	});

	it('movie 左缘为 42px 胶片条，其他票型为 4px 色条 ×3 = 12px，内容随之右移', () => {
		const movie = layoutShare('movie');
		const train = layoutShare('train');
		expect(movie.edgeW).toBe(EDGE_W_MOVIE);
		expect(train.edgeW).toBe(EDGE_W);
		expect(movie.content.x - train.content.x).toBe(EDGE_W_MOVIE - EDGE_W);
	});

	it('撕票线在卡内、脚注区在其下方', () => {
		const { card, tearY, foot } = layoutShare('show');
		expect(tearY).toBeGreaterThan(card.y);
		expect(tearY).toBeLessThan(card.y + card.h);
		expect(foot.y).toBe(tearY);
		expect(foot.y + foot.h).toBe(card.y + card.h);
	});
});

describe('filmHoleCenters', () => {
	it('按节距均匀铺满，孔心落在区间内', () => {
		const holes = filmHoleCenters(0, 100, 36);
		expect(holes).toEqual([18, 54]); // floor(100/36) = 2 个整孔，最后不足一节距不画
		expect(holes.length).toBe(Math.floor(100 / FILM_HOLE_PITCH));
		for (const y of holes) expect(y).toBeGreaterThan(0);
	});

	it('非法区间 → 空', () => {
		expect(filmHoleCenters(100, 0)).toEqual([]);
		expect(filmHoleCenters(0, 100, 0)).toEqual([]);
	});
});

describe('buildShareModel / shareFileName', () => {
	it('金额按整数分渲染，不带方向符号', () => {
		const model = buildShareModel(fixtureOf('movie'));
		expect(model.amount).toBe('¥99.00');
		expect(model.kindLabel).toBe('电影');
		expect(model.kindEmoji).toBe('🎬');
		expect(model.title).toBe('沙丘 3');
	});

	it('rating 收敛到 0-5', () => {
		const base = fixtureOf('train');
		expect(buildShareModel({ ...base, rating: 9 }).rating).toBe(5);
		expect(buildShareModel({ ...base, rating: -1 }).rating).toBe(0);
	});

	it('五种票型都能建模且必备文案非空', () => {
		for (const ticket of fixtureTickets) {
			const model = buildShareModel(ticket);
			expect(model.title).not.toBe('');
			expect(model.time).toMatch(/^\d{4}-\d{2}-\d{2} \d{2}:\d{2}$/);
			expect(model.amount.startsWith('¥')).toBe(true);
			expect(model.kindColor).toMatch(/^#[0-9a-f]{6}$/);
		}
	});

	it('文件名去掉路径/非法字符并带日期', () => {
		const model = buildShareModel({ ...fixtureOf('movie'), title: 'a/b:c 片' });
		expect(shareFileName(model)).toMatch(/^a-b-c-片-\d{4}-\d{2}-\d{2}\.png$/);
	});
});
