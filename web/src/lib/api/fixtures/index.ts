/**
 * fixtures 唯一出口：JSON → 契约类型。
 * - categories 与 server/migrations/0002_categories.up.sql 的 seed 完全一致（id 1-11）
 * - tickets 覆盖五种 kind；transactions 27 条、跨 7 天、含 5 条票关联
 * - stats-*.json 由 transactions/tickets 推导生成，数字互相对账一致
 */
import type { Category, MonthlyStats, Ticket, TicketStats, Transaction, User } from '../types';
import categoriesJson from './categories.json';
import statsMonthlyJson from './stats-monthly.json';
import statsTicketsJson from './stats-tickets.json';
import ticketsJson from './tickets.json';
import transactionsJson from './transactions.json';
import userJson from './user.json';

export const fixtureUser = userJson as User;
export const fixtureCategories = categoriesJson as unknown as Category[];
export const fixtureTransactions = transactionsJson as unknown as Transaction[];
export const fixtureTickets = ticketsJson as unknown as Ticket[];
export const fixtureStatsMonthly = statsMonthlyJson as unknown as MonthlyStats;
export const fixtureStatsTickets = statsTicketsJson as unknown as TicketStats;

/** fixtures 数据所在的月份 / 年份（mock 对其他区间返回空数据） */
export const FIXTURE_MONTH = '2026-07';
export const FIXTURE_YEAR = 2026;
