import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vitest/config';

export default defineConfig({
	plugins: [
		sveltekit({
			compilerOptions: {
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			// SPA: every route is client-rendered from index.html, which the Go server embeds.
			adapter: adapter({ pages: 'build', assets: 'build', fallback: 'index.html', strict: false })
		})
	],
	server: {
		proxy: {
			'/api': 'http://127.0.0.1:8080',
			'/vendor': 'http://127.0.0.1:8080'
		}
	},
	test: {
		include: ['src/**/*.test.ts']
	}
});
