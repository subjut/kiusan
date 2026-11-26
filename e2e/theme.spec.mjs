// The header's theme toggle in a real browser: theme.js keeps the page
// in sync after load (live system changes while no cookie is set, the
// toggle pinning a choice that outlives them) — jsdom has no media
// emulation, so nothing covers it there. The no-flash contract (a stored
// choice rendered as a class on <html>) is pinned through a plain
// response with the cookie.
import { test, expect } from "@playwright/test";

test("follows the system scheme while no cookie is set", async ({ page }) => {
	await page.emulateMedia({ colorScheme: "light" });
	await page.goto("/");
	await expect(page.locator("html")).toHaveClass(/theme-light/);

	// A live system change re-applies (the matchMedia listener).
	await page.emulateMedia({ colorScheme: "dark" });
	await expect(page.locator("html")).toHaveClass(/theme-dark/);
	await page.emulateMedia({ colorScheme: "light" });
	await expect(page.locator("html")).toHaveClass(/theme-light/);

	// Following the system mints no cookie.
	expect(
		(await page.context().cookies()).some((c) => c.name === "kiusan-theme")
	).toBe(false);
});

test("the toggle pins a choice that outlives system changes", async ({ page }) => {
	await page.emulateMedia({ colorScheme: "light" });
	await page.goto("/");
	await expect(page.locator("html")).toHaveClass(/theme-light/);

	await page.click("#theme-toggle");
	await expect(page.locator("html")).toHaveClass(/theme-dark/);
	expect(
		(await page.context().cookies()).some(
			(c) => c.name === "kiusan-theme" && c.value === "dark"
		)
	).toBe(true);
	// The button offers the switch to the other theme.
	await expect(page.locator("#theme-toggle")).toHaveAttribute(
		"aria-label",
		"Switch to light mode"
	);

	// The pin wins over the system setting.
	await page.emulateMedia({ colorScheme: "light" });
	await expect(page.locator("html")).toHaveClass(/theme-dark/);
});

test("a stored choice renders with the page (no flash)", async ({ browser }) => {
	const ctx = await browser.newContext({
		storageState: {
			cookies: [
				{
					name: "kiusan-theme",
					value: "dark",
					domain: "127.0.0.1",
					path: "/",
				},
			],
		},
	});
	const body = await (await ctx.request.get("/")).text();
	// The server renders the class from the cookie: the first paint is
	// already dark, no JS involved.
	expect(body).toMatch(/<html lang="en" class="theme-dark">/);
	await ctx.close();
});
