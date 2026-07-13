/**
 * 当前登录用户信息的本地持久化（key: piaoju.user）。
 *
 * 契约里没有 GET /me —— User 只随 login/register 响应下发（PROTOCOL §2），
 * 所以登录/注册成功后把 data.user 存这里，/me 页读取展示。
 * token 本体仍由 $lib/api/tokens.ts 独管（piaoju.access / piaoju.refresh）。
 */
import type { User } from '$lib/api/types';
import { tokenStore } from '$lib/api/tokens';

export const USER_KEY = 'piaoju.user';

const storage = (): Storage | null => (typeof localStorage === 'undefined' ? null : localStorage);

export function saveUser(user: User): void {
	storage()?.setItem(USER_KEY, JSON.stringify(user));
}

export function loadUser(): User | null {
	const raw = storage()?.getItem(USER_KEY);
	if (!raw) return null;
	try {
		return JSON.parse(raw) as User;
	} catch {
		return null;
	}
}

export function clearUser(): void {
	storage()?.removeItem(USER_KEY);
}

/** 是否处于登录态：有 refresh token 即算（access 过期可被 client 自动续期） */
export function hasSession(): boolean {
	return tokenStore.refresh !== null;
}
