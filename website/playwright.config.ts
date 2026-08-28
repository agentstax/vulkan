import { defineConfig, devices } from '@playwright/test';

// the three engine families of the supported line (CONVENTIONS ## Browser
// support); flows run against the built site because search and the pagefind
// index exist only in build output
export default defineConfig({
	testDir: './tests',
	fullyParallel: true,
	use: {
		baseURL: 'http://localhost:4321',
	},
	projects: [
		{ name: 'chromium', use: { ...devices['Desktop Chrome'] } },
		{ name: 'firefox', use: { ...devices['Desktop Firefox'] } },
		{ name: 'webkit', use: { ...devices['Desktop Safari'] } },
	],
	webServer: {
		command: 'npm run build && npm run preview',
		url: 'http://localhost:4321',
		reuseExistingServer: !process.env.CI,
		timeout: 240_000,
	},
});
