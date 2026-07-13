/**
 * W4 验收测试：通过真实 httpApi（vitest 下 VITE_MOCK='' → api = httpApi）
 * 用桩 fetch 逐包核对认证链路 —— 与登录/注册/我的 三页调用的正是同一条代码路径：
 *
 * 1. login/register 成功 → tokens.ts 持久化 token 对 + session.ts 缓存 User
 * 2. 40101 → 自动 POST /auth/refresh → 原请求带新 access 重试一次（token 旋转落盘）
 * 3. refresh 失效（40102）→ 本地 token 清空 + 错误上抛（页面层跳登录）
 * 4. logout → 无论服务端成败本地 token 清空（/me 页随后 clearUser）
 * 5. 40901 信封 → ApiError(EMAIL_TAKEN)（注册页挂到邮箱字段的分支）
 */
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { api, ApiError, tokenStore } from '../../lib/api/client';
import { ERR, type User } from '../../lib/api/types';
import { clearUser, hasSession, loadUser, saveUser } from './session';

/* ---- node 环境无 localStorage：给 tokens.ts / session.ts 一个内存实现 ---- */
function memStorage(): Storage {
	const map = new Map<string, string>();
	return {
		get length() {
			return map.size;
		},
		clear: () => map.clear(),
		getItem: (key) => map.get(key) ?? null,
		key: (index) => [...map.keys()][index] ?? null,
		removeItem: (key) => {
			map.delete(key);
		},
		setItem: (key, value) => {
			map.set(key, String(value));
		}
	};
}

interface Envelope {
	code: number;
	message: string;
	data: unknown;
}

interface SeenRequest {
	url: string;
	method: string;
	auth: string | null;
	body: unknown;
}

/** 按调用顺序回放信封响应，并记录每个请求的 url/method/Authorization/body */
function stubFetch(responses: Envelope[]): SeenRequest[] {
	const seen: SeenRequest[] = [];
	vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
		const headers = new Headers(init?.headers);
		const raw = typeof init?.body === 'string' ? init.body : null;
		seen.push({
			url: String(input),
			method: init?.method ?? 'GET',
			auth: headers.get('authorization'),
			body: raw ? (JSON.parse(raw) as unknown) : null
		});
		const envelope = responses[seen.length - 1];
		if (!envelope) throw new Error(`unexpected fetch #${seen.length}: ${String(input)}`);
		return new Response(JSON.stringify(envelope), {
			status: 200,
			headers: { 'content-type': 'application/json' }
		});
	});
	return seen;
}

const fixtureUser: User = {
	id: 1,
	email: 'leon@piaoju.app',
	nickname: '阿灯',
	createdAt: '2026-06-20T03:24:00Z'
};

const ok = (data: unknown): Envelope => ({ code: 0, message: 'ok', data });

beforeEach(() => {
	vi.stubGlobal('localStorage', memStorage());
});

afterEach(() => {
	vi.unstubAllGlobals();
});

describe('登录页链路：login 成功 → token 持久化 + 用户缓存', () => {
	it('token 经 tokens.ts 落盘，User 经 session.ts 可回读', async () => {
		const seen = stubFetch([
			ok({ user: fixtureUser, accessToken: 'acc-1', refreshToken: 'ref-1' })
		]);

		const data = await api.login({ email: 'leon@piaoju.app', password: 'password1' });
		saveUser(data.user); // 登录页 submit 内的同一步骤

		expect(seen[0]).toMatchObject({
			url: '/api/v1/auth/login',
			method: 'POST',
			auth: null, // auth 接口自身不带 Authorization
			body: { email: 'leon@piaoju.app', password: 'password1' }
		});
		expect(tokenStore.access).toBe('acc-1');
		expect(tokenStore.refresh).toBe('ref-1');
		expect(hasSession()).toBe(true);
		expect(loadUser()).toEqual(fixtureUser);
	});
});

describe('40101 → 自动 refresh → 原请求重试（client 内置，经页面同路径触发）', () => {
	it('重试带新 access，token 对旋转落盘', async () => {
		tokenStore.set('acc-stale', 'ref-1');
		const seen = stubFetch([
			{ code: ERR.TOKEN_EXPIRED, message: 'token expired', data: null },
			ok({ accessToken: 'acc-2', refreshToken: 'ref-2' }),
			ok({ items: [] })
		]);

		const items = await api.listCategories();

		expect(items).toEqual([]);
		expect(seen).toHaveLength(3);
		expect(seen[0]).toMatchObject({ url: '/api/v1/categories', auth: 'Bearer acc-stale' });
		expect(seen[1]).toMatchObject({
			url: '/api/v1/auth/refresh',
			method: 'POST',
			auth: null,
			body: { refreshToken: 'ref-1' }
		});
		expect(seen[2]).toMatchObject({ url: '/api/v1/categories', auth: 'Bearer acc-2' });
		expect(tokenStore.access).toBe('acc-2');
		expect(tokenStore.refresh).toBe('ref-2');
	});

	it('refresh 失效（40102）→ 清空本地 token 并上抛，页面层可跳登录', async () => {
		tokenStore.set('acc-stale', 'ref-dead');
		stubFetch([
			{ code: ERR.TOKEN_EXPIRED, message: 'token expired', data: null },
			{ code: ERR.REFRESH_INVALID, message: 'refresh token 无效', data: null }
		]);

		await expect(api.listCategories()).rejects.toMatchObject({
			name: 'ApiError',
			code: ERR.REFRESH_INVALID
		});
		expect(tokenStore.access).toBeNull();
		expect(tokenStore.refresh).toBeNull();
		expect(hasSession()).toBe(false);
	});
});

describe('/me 页链路：logout', () => {
	it('成功登出 → token 与用户缓存全清', async () => {
		tokenStore.set('acc-1', 'ref-1');
		saveUser(fixtureUser);
		const seen = stubFetch([ok(null)]);

		await api.logout();
		clearUser(); // /me 页 doLogout 内的同一步骤

		expect(seen[0]).toMatchObject({
			url: '/api/v1/auth/logout',
			method: 'POST',
			body: { refreshToken: 'ref-1' }
		});
		expect(tokenStore.access).toBeNull();
		expect(tokenStore.refresh).toBeNull();
		expect(loadUser()).toBeNull();
	});

	it('服务端报错也不影响本地登出（client finally 清 token）', async () => {
		tokenStore.set('acc-1', 'ref-1');
		saveUser(fixtureUser);
		stubFetch([{ code: ERR.INTERNAL, message: 'boom', data: null }]);

		await expect(api.logout()).rejects.toBeInstanceOf(ApiError);
		clearUser(); // /me 页 catch 后继续执行的同一步骤

		expect(tokenStore.access).toBeNull();
		expect(tokenStore.refresh).toBeNull();
		expect(loadUser()).toBeNull();
	});
});

describe('注册页链路：40901 邮箱已注册', () => {
	it('信封错误还原为 ApiError(EMAIL_TAKEN)，不落 token', async () => {
		stubFetch([{ code: ERR.EMAIL_TAKEN, message: '邮箱已注册', data: null }]);

		await expect(
			api.register({ email: 'leon@piaoju.app', password: 'password1', nickname: '阿灯' })
		).rejects.toMatchObject({ name: 'ApiError', code: ERR.EMAIL_TAKEN });
		expect(tokenStore.access).toBeNull();
		expect(hasSession()).toBe(false);
	});
});
