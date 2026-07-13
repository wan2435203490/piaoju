/**
 * 同步事件总线（W5）。
 *
 * 独立成模块、不 import Dexie —— 页面可以静态订阅它而不把 IndexedDB 代码拖进首屏包
 * （sync-engine 是动态加载的，页面无法静态 import 它的导出）。
 *
 * 用途：后台 pull 把新数据合并进本地库后，页面据此重新读取；push 队列变化后，
 * 「待同步」标记据此更新。
 */

type Listener = () => void;

const listeners = new Set<Listener>();

/** 订阅同步完成事件。返回退订函数（组件 onMount 里 return 它即可）。 */
export function onSync(cb: Listener): () => void {
	listeners.add(cb);
	return () => listeners.delete(cb);
}

/** 由 sync-engine 在一轮 push/pull 有实际变更后调用 */
export function emitSync(): void {
	for (const cb of listeners) {
		try {
			cb();
		} catch (err) {
			console.warn('[piaoju] sync listener failed', err);
		}
	}
}
