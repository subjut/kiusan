// The vote golden path — full mirror of the meeting's: create a yes/no
// vote through the chooser UI (the options added row by row, the
// mode-checkbox interlock exercised on the way), a fresh identity
// answers through the user link, and the organizer's matrix + totals
// pick the answer up.
import { test, expect } from "@playwright/test";

test("create -> answer -> organizer view", async ({ page, browser }) => {
	// ---- The organizer creates the vote through the form UI ----
	await page.goto("/vote-form");
	await page.fill("#title", "E2E journey vote");
	await page.fill("#email", "journey@kiusan.test");

	// Three options: the seeded row plus two added through the chooser.
	const rows = page.locator("#choice-rows .choice-row input[type=text]");
	await rows.nth(0).fill("Option A");
	await page.click("#add-choice");
	await rows.nth(1).fill("Option B");
	await page.click("#add-choice");
	await rows.nth(2).fill("Option C");
	await expect(page.locator("#choice-count")).toHaveText("3 / 20");

	// The mode interlock: single-choice greys out yes/no + yes/maybe/no
	// and moves the selection to the mode's default (yes).
	await page.check("#single-choice");
	await expect(page.locator("#votetype-yes")).toBeChecked();
	await expect(page.locator("#votetype-yes-no")).toBeDisabled();
	await expect(page.locator("#votetype-yes-maybe-no")).toBeDisabled();
	// Back to the plain yes/no vote the rest of the journey expects.
	await page.uncheck("#single-choice");
	await page.check("#votetype-yes-no");

	const submit = page.locator("#form input[type=submit]");
	await expect(submit).toBeEnabled();
	await submit.click();

	// The organizer lands on the admin page, where both links live.
	await expect(page).toHaveURL(/\/vote\/[^/]+$/);
	await expect(page.locator("h2").first()).toHaveText("E2E journey vote");
	const boxes = page.locator("input.copybox");
	await expect(boxes).toHaveCount(2);
	const [userLink, adminLink] = await boxes.evaluateAll((i) =>
		i.map((b) => b.value)
	);
	expect(userLink).toMatch(/\/vote\//);
	expect(new URL(adminLink).pathname).toBe(new URL(page.url()).pathname);

	// The three choice columns, each with a yes / no toggle in the form row.
	await expect(page.locator("thead th.th-choice")).toHaveCount(3);
	await expect(page.locator("td.form-cell")).toHaveCount(3);

	// ---- A fresh identity answers through the user link ----
	const ctx = await browser.newContext();
	const resp = await ctx.newPage();
	await resp.goto(userLink);
	const key = new URL(userLink).pathname.split("/").pop();
	expect((await ctx.cookies()).some((c) => c.name === "kiusan-" + key)).toBe(true);

	await resp.fill("#name", "E2E Bob");
	// Yes on A, no on C.
	await resp.click("td.form-cell >> nth=0 >> .mark-g");
	await resp.click("td.form-cell >> nth=2 >> .mark-r");
	await expect(resp.locator("td.form-cell").nth(0)).toHaveAttribute("data-status", "y");
	await resp.click("#answer-form button[type=submit]");
	await expect(resp).toHaveURL(userLink);

	const own = resp.locator("tbody tr", { hasText: "E2E Bob" });
	await expect(own).toHaveCount(1);
	await expect(own.locator("td").first()).toContainText("you");
	await expect(own.locator("td.st-g")).toHaveCount(1);
	await expect(own.locator("td.st-r")).toHaveCount(1);
	// The per-type totals: the Yes row counts A, the No row counts C.
	const totals = resp.locator("tr.totals-row");
	await expect(totals).toHaveCount(2);
	await expect(totals.nth(0).locator("td.totals-cell").nth(0)).toHaveText("1");
	await expect(totals.nth(1).locator("td.totals-cell").nth(2)).toHaveText("1");

	// ---- The organizer's matrix picks the answer up ----
	await page.goto(adminLink);
	await expect(page.locator("tbody tr", { hasText: "E2E Bob" })).toHaveCount(1);
	await expect(page.locator(".del-btn")).toHaveCount(1);
	const aTotals = page.locator("tr.totals-row");
	await expect(aTotals.nth(0).locator("td.totals-cell").nth(0)).toHaveText("1");
	await expect(aTotals.nth(1).locator("td.totals-cell").nth(2)).toHaveText("1");
	await ctx.close();
});
