/**
 * 原生壳初始化（Wave 5 P1）—— 状态栏 + 启动图。
 *
 * 只在原生 App 内生效；Web/PWA 是 no-op。插件走动态 import，Web bundle 不含。
 * 状态栏颜色跟随 design 的亮/暗 token，随系统主题切换。
 */
import { Capacitor } from '@capacitor/core';

// design tokens（app.css）：亮色纸白 / 暗色近黑。原生状态栏不能读 CSS 变量，镜像值。
const BG_LIGHT = '#faf6ef';
const BG_DARK = '#171412';

/** 应用挂载时调用一次。非原生环境直接返回。 */
export async function initNativeShell(): Promise<void> {
	if (!Capacitor.isNativePlatform()) return;
	try {
		const [{ StatusBar, Style }, { SplashScreen }] = await Promise.all([
			import('@capacitor/status-bar'),
			import('@capacitor/splash-screen')
		]);

		const applyStatusBar = async (dark: boolean) => {
			// Style.Dark = 深色状态栏图标（配亮底）；Style.Light = 浅色图标（配暗底）
			await StatusBar.setStyle({ style: dark ? Style.Light : Style.Dark });
			await StatusBar.setBackgroundColor({ color: dark ? BG_DARK : BG_LIGHT });
		};

		const mq = window.matchMedia('(prefers-color-scheme: dark)');
		await applyStatusBar(mq.matches);
		mq.addEventListener('change', (e) => void applyStatusBar(e.matches));

		// web 资源就绪后再收起启动图，避免白屏闪烁
		await SplashScreen.hide();
	} catch (err) {
		console.warn('[piaoju] 原生壳初始化失败（不影响功能）', err);
	}
}
