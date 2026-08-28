import { expect, test } from '@playwright/test';

// PGlite downloads and boots a full Postgres; its waits dwarf the default
const sandboxBootTimeout = 120_000;

test('the home page renders with a board style applied', async ({ page }) => {
	await page.goto('/');
	await expect(page).toHaveTitle(/Vulkan/);
	await expect(page.locator('html')).toHaveAttribute('data-board-style', /classic|night/);
});

test('the sandbox boots and its panels show rows', async ({ page }) => {
	test.setTimeout(sandboxBootTimeout + 30_000);
	await page.goto('/');
	await expect(page.locator('.sandbox-region table').first()).toBeVisible({
		timeout: sandboxBootTimeout,
	});
});

test('search finds threads and back returns to the results', async ({ page }) => {
	await page.goto('/search/?q=topic');
	const results = page.locator('.search-results a');
	await expect(results.first()).toBeVisible({ timeout: 20_000 });

	await results.first().click();
	await expect(page).not.toHaveURL(/\/search/, { timeout: 15_000 });

	// one back returns to the query and its results (the history entry keeps
	// the client router's state, so the router must handle the popstate)
	await page.goBack();
	await expect(page).toHaveURL(/\/search\/\?q=topic/);
	await expect(results.first()).toBeVisible({ timeout: 20_000 });
});

test('leaving a booted sandbox keeps the next page alive', async ({ page }) => {
	test.setTimeout(sandboxBootTimeout + 60_000);
	await page.goto('/');
	await expect(page.locator('.sandbox-region table').first()).toBeVisible({
		timeout: sandboxBootTimeout,
	});

	// closing PGlite with a statement in flight once wedged the main thread
	// for good; typing and searching proves the destination page still runs
	await page.locator('a[href="/search/"]').first().click();

	// an island still carrying ssr has not hydrated: typing into it submits
	// the form natively and the query is lost
	await expect(page.locator('astro-island[component-url*="board-search"]:not([ssr])')).toBeAttached(
		{ timeout: 15_000 },
	);
	await page.locator('input[type="search"]').fill('topic', { timeout: 15_000 });
	await page.keyboard.press('Enter');
	await expect(page.locator('.search-results a').first()).toBeVisible({ timeout: 20_000 });
});
