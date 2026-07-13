/// <reference types="@sveltejs/kit" />
/// <reference lib="webworker" />

/**
 * Service Worker（W5）—— 让 app 在没网时也能打开。
 *
 * 职责边界：**只缓存 app shell**（JS/CSS/字体/图标等构建产物 + static 文件）。
 * 业务数据的离线能力由 Dexie 本地库负责（lib/db），SW 一律不碰 /api：
 * 缓存 API 响应会与本地库形成两份真相，冲突时谁也说不清谁新。
 *
 * 策略
 * - 构建产物（带 hash，内容不可变）：cache-first，永不回源
 * - static 文件与导航请求：network-first，失败回退缓存（离线时给 SPA 的 index.html）
 * - 其它（含 /api、/uploads）：直接走网络，SW 不介入
 *
 * 每次构建 version 变化 → 新缓存名 → activate 时清掉旧版本，不留垃圾。
 */
import { build, files, version } from '$service-worker';

const sw = self as unknown as ServiceWorkerGlobalScope;

const CACHE = `piaoju-${version}`;
/** 构建产物路径（带内容 hash） */
const IMMUTABLE = new Set(build);
/**
 * 预缓存清单：app chunks + static 资源 + SPA 入口。
 * index.html 是 adapter-static 的 fallback，离线导航全靠它兜底，必须预缓存。
 */
const PRECACHE = [...build, ...files, '/index.html'];

sw.addEventListener('install', (event) => {
	event.waitUntil(
		caches
			.open(CACHE)
			.then((cache) => cache.addAll(PRECACHE))
			// 新版本装好即接管，不等用户关掉所有标签页
			.then(() => sw.skipWaiting())
	);
});

sw.addEventListener('activate', (event) => {
	event.waitUntil(
		caches
			.keys()
			.then((keys) => Promise.all(keys.filter((k) => k !== CACHE).map((k) => caches.delete(k))))
			.then(() => sw.clients.claim())
	);
});

sw.addEventListener('fetch', (event) => {
	const { request } = event;
	if (request.method !== 'GET') return;

	const url = new URL(request.url);
	if (url.origin !== location.origin) return;

	// API 与上传文件不进缓存：数据离线由 Dexie 负责，签名 URL 有时效
	if (url.pathname.startsWith('/api') || url.pathname.startsWith('/uploads')) return;

	event.respondWith(respond(request, url));
});

async function respond(request: Request, url: URL): Promise<Response> {
	const cache = await caches.open(CACHE);

	// 构建产物带内容 hash：命中即用，永不回源
	if (IMMUTABLE.has(url.pathname)) {
		const hit = await cache.match(url.pathname);
		if (hit) return hit;
	}

	// 其余（static 文件、SPA 导航）：网络优先，拿到就顺手更新缓存
	try {
		const response = await fetch(request);
		if (response.ok && response.type === 'basic') {
			cache.put(request, response.clone());
		}
		return response;
	} catch (err) {
		// 离线：回退缓存；导航请求回退到 SPA 的 index.html（客户端路由自己接管）
		const hit = (await cache.match(request)) ?? (await cache.match('/index.html'));
		if (hit) return hit;
		throw err;
	}
}
