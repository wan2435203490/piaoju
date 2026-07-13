/**
 * TicketCard 家族渲染冒烟：fixtures 五种 kind 全部走一遍 SSR render，
 * 验证「五种票型都能正确渲染」（W3 验收项）+ 票根视觉结构存在。
 */
import { describe, expect, it } from 'vitest';
import { render } from 'svelte/server';
import type { Ticket, TicketKind } from '$lib/api/types';
import { fixtureTickets } from '$lib/api/fixtures';
import TicketCard from './TicketCard.svelte';

function renderTicket(ticket: Ticket): string {
	return render(TicketCard, { props: { ticket } }).body;
}

function fixtureOf(kind: TicketKind): Ticket {
	const ticket = fixtureTickets.find((t) => t.kind === kind);
	if (!ticket) throw new Error(`fixtures 缺少 ${kind} 票样本`);
	return ticket;
}

describe('TicketCard 按 kind 分派渲染（fixtures 五种票型）', () => {
	it('movie：标题 + 影厅/制式 + 金额 + 整卡链接到详情', () => {
		const ticket = fixtureOf('movie');
		const html = renderTicket(ticket);
		expect(html).toContain('沙丘 3');
		expect(html).toContain('IMAX');
		expect(html).toContain('-¥99.00');
		expect(html).toContain(`href="/tickets/${ticket.id}"`);
		expect(html).toContain('movie'); // 胶片齿孔条的 movie 专属 class
	});

	it('show：标题 + 巡演 + 区域', () => {
		const html = renderTicket(fixtureOf('show'));
		expect(html).toContain('恋爱的犀牛');
		expect(html).toContain('蜂巢剧场');
		expect(html).toContain('-¥280.00');
	});

	it('attraction：标题 + 票种', () => {
		const html = renderTicket(fixtureOf('attraction'));
		expect(html).toContain('灵隐寺（飞来峰景区）');
		expect(html).toContain('成人票（含飞来峰）');
	});

	it('train：车次 + 出发/到达站横排布局', () => {
		const html = renderTicket(fixtureOf('train'));
		expect(html).toContain('G102');
		expect(html).toContain('杭州东');
		expect(html).toContain('北京南');
		expect(html).toContain('二等座');
		expect(html).toContain('-¥623.00');
	});

	it('flight：航班号 + 航司 + 出发/到达机场 + 舱位', () => {
		const html = renderTicket(fixtureOf('flight'));
		expect(html).toContain('MU5137');
		expect(html).toContain('中国东方航空');
		expect(html).toContain('上海虹桥 T2');
		expect(html).toContain('北京首都 T2');
		expect(html).toContain('经济舱 Y');
	});

	it('评分脚注：5 星电影票渲染 5 个实星', () => {
		const html = renderTicket(fixtureOf('movie'));
		expect(html.split('★').length - 1).toBe(5);
	});

	it('link=false（详情页顶卡）不输出链接', () => {
		const ticket = fixtureOf('movie');
		const html = render(TicketCard, { props: { ticket, link: false } }).body;
		expect(html).not.toContain('href=');
		expect(html).toContain('<article');
	});
});
