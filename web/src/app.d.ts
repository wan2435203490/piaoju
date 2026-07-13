// See https://svelte.dev/docs/kit/types#app.d.ts
// for information about these interfaces
declare global {
	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}
}

// Vite 环境变量（.env / 命令行注入）
interface ImportMetaEnv {
	/** '1' = 全部 API 走本地 fixtures mock（见 src/lib/api/mock.ts） */
	readonly VITE_MOCK?: string;
	/** API 前缀，默认 /api/v1（dev 走 vite proxy → localhost:8080） */
	readonly VITE_API_BASE?: string;
}

export {};
