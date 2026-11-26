// The copyable link boxes' copy buttons in a real browser: navigator
// clipboard needs a secure context (127.0.0.1 qualifies) and a granted
// permission — jsdom has no clipboard API at all. The boxes are read
// from a seeded table, so the test needs no poll of its own.
import { test, expect } from "@playwright/test";
import { seed } from "./env.mjs";

test("the copy buttons put the link box's URL on the clipboard", async ({ page, context }) => {
	await context.grantPermissions(["clipboard-read", "clipboard-write"], {
		origin: "http://127.0.0.1:8080",
	});

	for (const [label, route] of [
		["meeting 100x100", "/meet/"],
		["vote yes/no", "/vote/"],
	]) {
		const t = seed(label);
		await page.goto(t.admin);
		const boxes = page.locator("input.copybox");
		const values = await boxes.evaluateAll((i) => i.map((b) => b.value));
		expect(values[0]).toMatch(new RegExp("^https?://.+" + route));
		expect(values[1]).toMatch(new RegExp("^https?://.+" + route));

		// Both buttons, both boxes (user link, admin link).
		for (let i = 0; i < 2; i++) {
			await page.locator(".copy-btn").nth(i).click();
			await expect(page.locator(".copy-btn").nth(i)).toHaveText("Copied!");
			expect(await page.evaluate(() => navigator.clipboard.readText())).toBe(
				values[i]
			);
		}
	}
});
