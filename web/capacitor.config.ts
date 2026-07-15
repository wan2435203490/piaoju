import type { CapacitorConfig } from '@capacitor/cli';

/**
 * Capacitor 配置（Wave 5，P1 打包）。
 *
 * 工程放在 web/ 下（而非 PLAN 设想的独立 mobile/）：Capacitor CLI 默认在有
 * package.json 的目录运作，与 SvelteKit 产物耦合紧，同目录管理最省事。
 * android/ 由 `npx cap add android` 生成在此目录下。
 *
 * webDir 指 SvelteKit static adapter 的产物（fallback: index.html 的 SPA）。
 * 打包前必须先 `pnpm build`，再 `npx cap sync android`。
 */
const config: CapacitorConfig = {
	appId: 'local.piaoju.app',
	appName: '拾光票局',
	webDir: 'build',
	// 生产 App 走内置静态资源；本地联调可临时改 server.url 指向 dev server
	android: {
		// APK 包体红线（conventions §5 精神延伸）：release APK < 6MB
		buildOptions: {}
	},
	plugins: {
		SplashScreen: {
			launchShowDuration: 600,
			backgroundColor: '#FAF6EF', // design --bg Light（纸白）
			showSpinner: false
		},
		StatusBar: {
			// 纸白底 + 深色图标（design 亮色模式）；暗色模式运行时再调
			style: 'LIGHT',
			backgroundColor: '#FAF6EF'
		}
	}
};

export default config;
