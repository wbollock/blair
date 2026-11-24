import { defineConfig, devices } from '@playwright/test';

// Runs against a live server: BLAIR_URL (default http://localhost:18080), fresh data dir.
// SETUP_URL must be the one-time setup link from the server log.
export default defineConfig({
	testDir: 'e2e',
	fullyParallel: false,
	workers: 1,
	retries: 0,
	reporter: [['list']],
	use: {
		baseURL: process.env.BLAIR_URL ?? 'http://localhost:18080',
		trace: 'retain-on-failure',
		screenshot: 'only-on-failure'
	},
	projects: [
		{ name: 'desktop', testMatch: /desktop\.spec\.ts/, use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 } } },
		{ name: 'mobile', testMatch: /mobile\.spec\.ts/, dependencies: ['desktop'], use: { ...devices['Pixel 7'] } }
	]
});
