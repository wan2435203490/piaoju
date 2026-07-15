/**
 * 原生相机桥接（Wave 5 P1）。
 *
 * 拍票在原生 App（Capacitor WebView）里走系统相机，Web 里回退 <input type="file">。
 * 调用方不感知平台差异：`capturePhoto()` 在 Web 环境返回 null，调用方据此触发
 * 原有的文件选择器。
 *
 * 包体红线（conventions §5）：@capacitor/camera 走**动态 import**，Web 构建里被
 * tree-shake 出 bundle；只有 @capacitor/core 的 `Capacitor` 平台判断静态引入（极小）。
 */
import { Capacitor } from '@capacitor/core';

/** 是否运行在原生壳内（Android/iOS App）。Web/PWA 为 false。 */
export function isNative(): boolean {
	return Capacitor.isNativePlatform();
}

/**
 * 原生环境：唤起系统相机拍一张，返回 jpeg File。
 * Web 环境：返回 null（调用方回退到 <input type="file">）。
 * 用户取消 / 权限拒绝：返回 null，不抛错（调用方按「没拍」处理）。
 */
export async function capturePhoto(): Promise<File | null> {
	if (!isNative()) return null;
	try {
		// 动态 import：只有原生环境才拉这段代码，Web bundle 不含
		const { Camera, CameraResultType, CameraSource } = await import('@capacitor/camera');
		const photo = await Camera.getPhoto({
			quality: 85,
			resultType: CameraResultType.Uri,
			source: CameraSource.Camera,
			// 契约 §6：服务端纯 Go 无法解码 heic，Capacitor Camera 默认输出 jpeg
			correctOrientation: true
		});
		if (!photo.webPath) return null;
		const resp = await fetch(photo.webPath);
		const blob = await resp.blob();
		return new File([blob], `ticket-${Date.now()}.jpg`, { type: 'image/jpeg' });
	} catch {
		// 用户取消 getPhoto 会 reject —— 视作「没拍」，静默回退
		return null;
	}
}
