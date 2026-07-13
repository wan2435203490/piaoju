/**
 * 客户端 UUIDv4（transactions/tickets 业务主键 —— conventions §1）。
 * 优先 crypto.randomUUID；老 WebView（非 https 等场景）兜底 getRandomValues 手拼。
 */
export function uuid(): string {
	const c = globalThis.crypto;
	if (typeof c?.randomUUID === 'function') {
		return c.randomUUID();
	}
	const bytes: Uint8Array = c.getRandomValues(new Uint8Array(16));
	bytes[6] = (bytes[6]! & 0x0f) | 0x40; // version 4
	bytes[8] = (bytes[8]! & 0x3f) | 0x80; // variant 10
	let hex = '';
	for (const byte of bytes) {
		hex += byte.toString(16).padStart(2, '0');
	}
	return `${hex.slice(0, 8)}-${hex.slice(8, 12)}-${hex.slice(12, 16)}-${hex.slice(16, 20)}-${hex.slice(20)}`;
}
