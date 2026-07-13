import adapter from '@sveltejs/adapter-static';
import tailwindcss from '@tailwindcss/vite';
import { sveltekit } from '@sveltejs/kit/vite';
import { loadEnv } from 'vite';
import { defineConfig } from 'vitest/config';

export default defineConfig(({ mode }) => {
	// envDir 用相对路径，避免引用 process（省掉 @types/node）
	const env = loadEnv(mode, '.', '');

	return {
		plugins: [
			tailwindcss(),
			sveltekit({
				compilerOptions: {
					// Svelte 6 之前全项目强制 runes 模式（node_modules 除外）
					runes: ({ filename }) =>
						filename.split(/[/\\]/).includes('node_modules') ? undefined : true
				},
				// SPA 模式：纯静态产物 + index.html fallback（ssr=false 见 src/routes/+layout.ts）
				adapter: adapter({ fallback: 'index.html' })
			})
		],
		define: {
			// 钉成构建期常量：未开 mock 时 mock.ts + fixtures 整体被摇树，不进生产包
			'import.meta.env.VITE_MOCK': JSON.stringify(env.VITE_MOCK ?? '')
		},
		server: {
			// 非 mock 模式本地联调：转发到 Go 后端（make dev → localhost:8080）
			proxy: {
				'/api': 'http://localhost:8080',
				'/uploads': 'http://localhost:8080'
			}
		},
		test: {
			include: ['src/**/*.test.ts'],
			environment: 'node' as const
		}
	};
});
