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
 *
 * 内网联调（连电脑本地后端）：build/sync 前 `export CAP_SERVER_URL=http://<电脑IP>:5173`，
 * App 直接加载电脑上的 vite dev server（页面与 /api 同源走 5173，vite 代理到 8080，
 * 避开 CORS/混合内容/明文三坑）。不设该变量则走内置静态资源（发布形态）。IP 不进仓库。
 */
declare const process: { env: Record<string, string | undefined> };
const devServerUrl = process.env.CAP_SERVER_URL;

const config: CapacitorConfig = {
	appId: 'local.piaoju.app',
	appName: '拾光票局',
	webDir: 'build',
	// CAP_SERVER_URL 设置时：App 加载电脑 dev server（明文 http，故开 cleartext）
	...(devServerUrl ? { server: { url: devServerUrl, cleartext: true } } : {}),
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
