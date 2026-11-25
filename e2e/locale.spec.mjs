// The locale contract in a real browser: the dropdown switches a page in
// place (no reload, the cookie pins the choice), and a page brought back
// by back/forward navigation shows the locale that is current when it
// comes back. The back/forward part is what jsdom cannot do — it has no
// bfcache, no real navigation — so the jsdom suite fakes it with a
// synthetic pageshow; this spec pins the user-visible behavior end to end
// against the real server and a real Chromium.
import { test, expect } from "@playwright/test";

test("a switch on one page is in place and follows back/forward restores", async ({ page }) => {
	// Home, in the browser's implicit language (headless Chromium: en-US).
	await page.goto("/");
	await expect(page.locator("h2").first()).toHaveText("Schedule Meetings");
	await expect(page.locator('label[for="locale"]')).toHaveText("Language:");

	// Into the meeting form.
	await page.getByRole("link", { name: /Schedule Meetings/ }).click();
	await expect(page).toHaveURL(/\/meet-form/);
	await expect(page.locator("h2").first()).toHaveText("Schedule a meeting");

	// Switch to German there.
	await page.selectOption("#locale", "de");
	// In place: the URL is untouched, the dictionary and the nodes flipped.
	await expect(page).toHaveURL(/\/meet-form/);
	await expect(page.locator("h2").first()).toHaveText("Meeting planen");
	await expect(page.locator('label[for="locale"]')).toHaveText("Sprache:");
	expect(await page.evaluate(() => window.I18N.lang)).toBe("de");
	const cookies = await page.context().cookies();
	expect(cookies.some((c) => c.name === "kiusan-locale" && c.value === "de")).toBe(true);

	// Back to the home page: the browser may serve it from the bfcache
	// (the client reconciles it) or re-fetch it (the server renders it) —
	// either way the content is the current locale.
	await page.goBack();
	await expect(page).toHaveURL(/\/$/);
	await expect(page.locator("h2").first()).toHaveText("Termine planen");
	await expect(page.locator('label[for="locale"]')).toHaveText("Sprache:");

	// And forward again: the form page comes back in German too.
	await page.goForward();
	await expect(page).toHaveURL(/\/meet-form/);
	await expect(page.locator("h2").first()).toHaveText("Meeting planen");
});

test("picking the implicit language un-pins (no locale cookie left)", async ({ page }) => {
	// Headless Chromium's implicit language is English.
	await page.goto("/");
	await expect(page.locator("h2").first()).toHaveText("Schedule Meetings");

	await page.selectOption("#locale", "de");
	await expect(page.locator('label[for="locale"]')).toHaveText("Sprache:");
	let cookies = await page.context().cookies();
	expect(cookies.some((c) => c.name === "kiusan-locale" && c.value === "de")).toBe(true);

	// Back to the implicit language: the pin is deleted.
	await page.selectOption("#locale", "en");
	await expect(page.locator('label[for="locale"]')).toHaveText("Language:");
	cookies = await page.context().cookies();
	expect(cookies.some((c) => c.name === "kiusan-locale")).toBe(false);
});
