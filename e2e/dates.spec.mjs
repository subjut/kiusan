// The date *values* on a real page: locale.js rewrites the
// server-rendered labels with the viewer's real ICU, always in the
// poll's zone (the viewer's own zone is never used for the values), and
// an in-session locale switch re-formats them. jsdom approximates this
// with Node's Intl over fixtures — the user-visible formatting is
// Chromium's ICU, pinned here against the seeded Berlin poll.
import { test, expect } from "@playwright/test";
import { seed } from "./env.mjs";

test("date values render in the poll's zone and reformat on a locale switch", async ({ page }) => {
	const t = seed("meeting 100x100"); // Europe/Berlin
	await page.goto(t.user);

	// The timezone line: the zone name plus its current UTC offset
	// (longOffset — DST-dependent, so computed live like locale.js does).
	const off = await page.evaluate(() => {
		const parts = new Intl.DateTimeFormat("en-US", {
			timeZone: "Europe/Berlin",
			timeZoneName: "longOffset",
		}).formatToParts(new Date());
		return parts.find((p) => p.type === "timeZoneName").value;
	});
	await expect(page.locator(".tz-name")).toHaveText(`Europe/Berlin (${off})`);

	// The first slot is 8:00 Berlin. The headless browser sits in UTC,
	// where that instant is 7:00 — so "8:00 AM" pins that the value is
	// formatted in the poll's zone, not the viewer's.
	await expect(page.locator(".th-time").first()).toHaveText("8:00 AM");

	// A locale switch re-formats the values: the date cells' title turns
	// German (long weekday, comma, day *with* its period — the English
	// title has no day period), and the medium end-date turns day-first.
	await page.selectOption("#locale", "de");
	await expect(page.locator(".th-date").first()).toHaveAttribute(
		"title",
		/^([A-ZÄÖÜa-z]+), \d+\./
	);
	await expect(page.locator(".f-date").first()).toHaveText(/^\d/);
});
