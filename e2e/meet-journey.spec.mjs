// The meeting golden path in a real browser: the organizer creates a
// poll through the dater form (calendar + per-date time row + submit
// gate), lands on the admin page with both link boxes, a second context
// — a fresh identity — answers through the user link with the shipped
// JS wiring, and the organizer's matrix + totals pick the answer up.
// This is the one layer that covers shipped JS + real form
// serialization + cookie/CSRF + server redirect + post-render state in
// a single chain (the jsdom suite has no server, the Go suite has no
// browser).
import { test, expect } from "@playwright/test";

test("create -> answer -> organizer view", async ({ page, browser }) => {
	// ---- The organizer creates the meeting through the form UI ----
	await page.goto("/meet-form");
	await page.fill("#title", "E2E journey meeting");
	await page.fill("#email", "journey@kiusan.test");
	await page.fill("#description", "Created by the e2e golden path");

	// Pick the 1st of next month (guaranteed future) via the calendar.
	const firstNext = await page.evaluate(() => {
		const d = new Date();
		const n = new Date(d.getFullYear(), d.getMonth() + 1, 1);
		return `${n.getFullYear()}-${String(n.getMonth() + 1).padStart(2, "0")}-01`;
	});
	await page.click("#cal-next");
	await page.click(`#cal-grid button.day[data-date="${firstNext}"]`);
	await expect(page.locator("#date-chips .chip")).toHaveCount(1);

	// Add a time to that date (the per-date row's input + add button).
	await page.fill(".date-times-row .date-time-input", "10:30");
	await page.click(".date-times-row .date-time-add");
	await expect(page.locator(".date-times-row .chips .chip")).toHaveCount(1);
	await expect(page.locator("#slot-preview")).toHaveText(/10:30/);

	// The submit gate: enabled once the picker carries a date.
	const submit = page.locator("#form input[type=submit]");
	await expect(submit).toBeEnabled();
	await submit.click();

	// The organizer lands on the admin page, where both links live.
	await expect(page).toHaveURL(/\/meet\/[^/]+$/);
	await expect(page.locator("h2").first()).toHaveText("E2E journey meeting");
	const boxes = page.locator("input.copybox");
	await expect(boxes).toHaveCount(2);
	const [userLink, adminLink] = await boxes.evaluateAll((i) =>
		i.map((b) => b.value)
	);
	expect(userLink).toMatch(/\/meet\//);
	expect(new URL(adminLink).pathname).toBe(new URL(page.url()).pathname);

	// ---- A fresh identity answers through the user link ----
	const ctx = await browser.newContext();
	const resp = await ctx.newPage();
	await resp.goto(userLink);
	// The identity cookie is minted on the page load.
	const key = new URL(userLink).pathname.split("/").pop();
	expect((await ctx.cookies()).some((c) => c.name === "kiusan-" + key)).toBe(true);

	await resp.fill("#name", "E2E Alice");
	// The green toggle feeds the hidden input the form submits.
	await resp.click("td.form-cell >> nth=0 >> .mark-g");
	await expect(resp.locator("td.form-cell").first()).toHaveAttribute("data-status", "g");
	await resp.click("#answer-form button[type=submit]");
	await expect(resp).toHaveURL(userLink);

	// The submitted row renders with the mark, and the Yes totals row
	// counts it.
	const own = resp.locator("tbody tr", { hasText: "E2E Alice" });
	await expect(own).toHaveCount(1);
	await expect(own.locator("td").first()).toContainText("you");
	await expect(own.locator("td.st-g")).toHaveCount(1);
	await expect(
		resp.locator("tr.totals-row").first().locator("td.totals-cell").first()
	).toHaveText("1");

	// ---- The organizer's matrix picks the answer up ----
	await page.goto(adminLink);
	await expect(page.locator("tbody tr", { hasText: "E2E Alice" })).toHaveCount(1);
	// The admin view carries the per-row answer management.
	await expect(page.locator(".del-btn")).toHaveCount(1);
	await expect(
		page.locator("tr.totals-row").first().locator("td.totals-cell").first()
	).toHaveText("1");
	await ctx.close();
});
