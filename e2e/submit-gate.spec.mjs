// The greyed-out submit buttons in a real browser: a disabled control
// swallows its own clicks (the CSS sets pointer-events: none on it), so
// the form-level handler hit-tests the click's real coordinates against
// the button's real rect. jsdom's rects are zero-sized, so its suite
// fakes the hit at 0,0 — the real geometry can only be pinned here.
// Both gates flash their hint (red, until the shake's animationend) and
// never navigate.
import { test, expect } from "@playwright/test";

// A real mouse click at the disabled button's center.
async function clickDisabled(page, locator) {
	await locator.scrollIntoViewIfNeeded();
	const box = await locator.boundingBox();
	await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2);
}

test("the meet form's greyed submit flashes the date hint and stays put", async ({ page }) => {
	await page.goto("/meet-form");
	const hint = page.locator("#date-hint");
	const submit = page.locator("#form input[type=submit]");
	await expect(hint).toBeVisible();
	await expect(submit).toBeDisabled();

	// The click lands on the form (pointer-events: none), the hit-test
	// matches, and the hint shakes — without any navigation.
	await clickDisabled(page, submit);
	await expect(page).toHaveURL(/\/meet-form$/);
	await expect(hint).toHaveClass(/err/);
	// The shake's animationend drops the red.
	await expect(hint).not.toHaveClass(/err/);

	// Once a date is picked, the gate lifts: hint hidden, submit live.
	await page.click("#cal-grid button.day:not(:disabled)");
	await expect(hint).toBeHidden();
	await expect(submit).toBeEnabled();
});

test("a must-answer vote's greyed submit flashes the hint until the answer is complete", async ({ page, request }) => {
	// A must-answer yes/no vote, created server-side (the form is not the
	// subject here — the page's gate is).
	const res = await request.post("/vote-form", {
		form: {
			title: "E2E gate vote",
			votetype: "yes-no",
			"must-answer": "true",
			choice0: "A",
			choice1: "B",
		},
	});
	expect(res.status()).toBe(200);
	const body = await (await request.get(new URL(res.url()).pathname)).text();
	const userLink = body.match(/class="copybox" readonly value="([^"]+)"/)[1];

	await page.goto(userLink);
	const hint = page.locator("#answer-form p.must-hint");
	const submit = page.locator("#answer-form button[type=submit]");
	await expect(hint).toBeVisible();
	await expect(submit).toBeDisabled();

	// The real-coords click flashes the hint; nothing is submitted.
	await clickDisabled(page, submit);
	await expect(page).toHaveURL(userLink);
	await expect(hint).toHaveClass(/err/);
	await expect(hint).not.toHaveClass(/err/);

	// A complete answer (a mark per option) unlocks the gate, and the
	// hint turns green.
	await page.click("td.form-cell >> nth=0 >> .mark-g");
	await page.click("td.form-cell >> nth=1 >> .mark-r");
	await expect(submit).toBeEnabled();
	await expect(hint).toHaveClass(/ok/);
});
