/**
 * token 持久化（localStorage）。独立成模块避免 client ↔ mock 循环依赖。
 * key 约定（W1 任务卡）：piaoju.access / piaoju.refresh
 */

export const TOKEN_KEY = {
	access: 'piaoju.access',
	refresh: 'piaoju.refresh'
} as const;

const storage = (): Storage | null => (typeof localStorage === 'undefined' ? null : localStorage);

export const tokenStore = {
	get access(): string | null {
		return storage()?.getItem(TOKEN_KEY.access) ?? null;
	},
	get refresh(): string | null {
		return storage()?.getItem(TOKEN_KEY.refresh) ?? null;
	},
	set(access: string, refresh: string): void {
		storage()?.setItem(TOKEN_KEY.access, access);
		storage()?.setItem(TOKEN_KEY.refresh, refresh);
	},
	clear(): void {
		storage()?.removeItem(TOKEN_KEY.access);
		storage()?.removeItem(TOKEN_KEY.refresh);
	}
};
