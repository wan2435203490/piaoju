/**
 * 时钟偏移校正（W5）—— 防「本地时钟慢」把离线改动永久卡死。
 *
 * 契约 §8 的 LWW 用 clientUpdatedAt 与服务端 updated_at 比大小，而服务端 updated_at
 * 一律由服务端时钟写入。若用户设备时钟慢了 10 分钟，本地改动的 clientUpdatedAt 就永远
 * 小于服务端已有版本 → 每次 push 都判 stale → 改动永远推不上去，且用户毫无察觉。
 *
 * 校正：pull 时观察服务端下发实体的 updatedAt（服务端时钟的样本）。若它超过本地此刻，
 * 说明本地慢了，记下差值；后续 clientUpdatedAt 一律加上这个偏移。
 *
 * 只补正、不回拨（本地快时不减）：本地快只会让自己在 LWW 里占优，不会卡死；
 * 而回拨会引入新的 stale 风险。取最大观测值，单调不减。
 */

let skewMs = 0;

/** 用一个服务端时间戳样本（RFC3339）校准 */
export function observeServerTime(serverIso: string): void {
	const server = Date.parse(serverIso);
	if (Number.isNaN(server)) return;
	const delta = server - Date.now();
	if (delta > skewMs) skewMs = delta;
}

/** 当前估算偏移（毫秒，>=0） */
export function clockSkewMs(): number {
	return skewMs;
}

/** 校正后的「现在」，RFC3339 UTC —— 所有 clientUpdatedAt 都该用它，而非裸 Date */
export function nowIso(): string {
	return new Date(Date.now() + skewMs).toISOString();
}
