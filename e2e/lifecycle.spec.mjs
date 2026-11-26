// The admin poll lifecycle in the real UI (both link shapes): close →
// the closed admin's final-toggle buttons (a JS-free POST via the HTML5
// form attribute) → the final frame on both links → the two-step
// re-open that clears the marks — and the two-step delete that purges
// the poll, drops the browser's identity cookie, and turns both links
// into the dedicated 404 gone page. The seeded tables are re-seeded on
// every run, so mutating them (one poll closed/deleted per file, other
// specs' polls left untouched) is safe.
import { test, expect } from "@playwright/test";
import { seed } from "./env.mjs";

test("meeting: close -> final -> frame on both links -> two-step reopen clears", async ({ page }) => {
	const t = seed("meeting 100x50");

	// ---- Close the poll (admin link only) ----
	await page.goto(t.admin);
	await page.click('button[data-i18n="meet.closePoll"]');
	await expect(page).toHaveURL(t.admin);
	await expect(page.locator(".closed")).toBeVisible();
	// The form row is gone: the table is a static matrix on both links.
	await expect(page.locator(".form-row")).toHaveCount(0);

	// ---- Mark the first slot final (the check-only toggle's JS-free
	//      form-attribute POST) ----
	await page.locator("tr.final-footer-row .final-btn").first().click();
	await expect(page).toHaveURL(t.admin);
	await expect(page.locator("tr.final-footer-row .final-btn").first()).toHaveAttribute(
		"aria-pressed",
		"true"
	);
	// The final column is framed: header + body cells.
	expect(await page.locator("th.final").count()).toBeGreaterThan(0);
	expect(await page.locator("td.cell.final").count()).toBeGreaterThan(0);

	// ---- The user link frames it too, with the rotated Final! label ----
	await page.goto(t.user);
	expect(await page.locator("td.cell.final").count()).toBeGreaterThan(0);
	await expect(page.locator("span.final-mark-text").first()).toBeVisible();

	// ---- Re-open with finals set: the two-step warning, then cleared ----
	await page.goto(t.admin);
	await page.click("button.reopen-poll-btn");
	await expect(page.locator(".reopen-warning")).toBeVisible();
	await page.click('button[type=submit][form="poll-reopen-form"]');
	await expect(page).toHaveURL(t.admin);
	await expect(page.locator(".closed")).toHaveCount(0);
	// The decision is invalidated: every final mark is gone.
	await expect(page.locator("td.cell.final")).toHaveCount(0);
});

test("meeting: the two-step delete purges the poll and the identity cookie", async ({ page, context }) => {
	const t = seed("meeting 100x50");
	const key = new URL(t.user).pathname.split("/").pop();

	// The identity cookie (shared by both links) is minted on page load.
	await page.goto(t.admin);
	expect((await context.cookies()).some((c) => c.name === "kiusan-" + key)).toBe(true);

	// Two-step: the trigger opens the inline warning, only the confirm
	// button submits.
	await page.click("button.delete-poll-btn");
	await expect(page.locator(".delete-warning")).toBeVisible();
	await page.click('button[type=submit][form="poll-del-form"]');
	// The delete redirects home and drops the identity cookie.
	await expect(page).toHaveURL(/\/$/);
	expect((await context.cookies()).some((c) => c.name === "kiusan-" + key)).toBe(false);

	// Both links are now the dedicated 404 gone page.
	for (const link of [t.admin, t.user]) {
		const res = await page.goto(link);
		expect(res.status()).toBe(404);
		await expect(page.locator('h2[data-i18n="meet.goneTitle"]')).toBeVisible();
	}
});

test("vote: close -> final option -> frame -> reopen; a single-choice vote keeps the feature", async ({ page }) => {
	// ---- A yes/maybe/no vote mirrors the meeting lifecycle ----
	const t = seed("vote yes/maybe/no");
	await page.goto(t.admin);
	await page.click('button[data-i18n="meet.closePoll"]');
	await expect(page).toHaveURL(t.admin);
	await expect(page.locator(".closed")).toBeVisible();
	await expect(page.locator(".form-row")).toHaveCount(0);

	await page.locator("tr.final-footer-row .final-btn").first().click();
	await expect(page).toHaveURL(t.admin);
	expect(await page.locator("td.cell.final").count()).toBeGreaterThan(0);

	await page.goto(t.user);
	expect(await page.locator("td.cell.final").count()).toBeGreaterThan(0);
	await expect(page.locator("span.final-mark-text").first()).toBeVisible();

	await page.goto(t.admin);
	await page.click("button.reopen-poll-btn");
	await expect(page.locator(".reopen-warning")).toBeVisible();
	await page.click('button[type=submit][form="poll-reopen-form"]');
	await expect(page.locator(".closed")).toHaveCount(0);
	await expect(page.locator("td.cell.final")).toHaveCount(0);

	// ---- A single-choice vote may carry final marks (only a
	//      single-signup meeting is barred) ----
	const s = seed("vote yes single-choice");
	await page.goto(s.admin);
	await page.click('button[data-i18n="meet.closePoll"]');
	await expect(page.locator(".closed")).toBeVisible();
	await page.locator("tr.final-footer-row .final-btn").first().click();
	await expect(page).toHaveURL(s.admin);
	expect(await page.locator("td.cell.final").count()).toBeGreaterThan(0);
});
