import { redirect } from '@sveltejs/kit';

// / → 默认首页 /ledger
export function load(): never {
	redirect(307, '/ledger');
}
