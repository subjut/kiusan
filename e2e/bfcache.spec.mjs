// A page brought back by back/forward navigation, with a locale change
// in between — on the answers-table page, the surface the locale spec
// never reached. The browser restores the page one of two ways (a
// bfcache hit, parked DOM with the scripts not re-executing; or a
// re-fetch with the scripts re-running over a stale render), and the
// app's contract is convergence on *both* (locale.js' pageshow handler):
// the current locale in the UI strings *and* the date values, the
// dropdown re-synced, and the answer form wired and usable. The restore
// mechanism itself is the browser's business — headless Chromium does
// not always park (a re-fetch loses parked form state, which is not an
// app contract), so the spec asserts the converged state, not the
// mechanism.
import { test, expect } from "@playwright/test";
import { seed } from "./env.mjs";

test("a locale-changed page comes back converged, form wiring included", async ({ page }) => {
	const t = seed("meeting 100x100");

	// The meeting page in the implicit (headless: en) locale.
	await page.goto(t.user);
	await expect(page.locator('label[for="locale"]')).toHaveText("Language:");

	// Forward to home, and switch the locale there (the cookie pins it).
	await page.goto("/");
	await page.selectOption("#locale", "de");
	await expect(page.locator('label[for="locale"]')).toHaveText("Sprache:");

	// Back to the meeting page — bfcache hit or re-fetch, it converges.
	await page.goBack();
	await expect(page).toHaveURL(t.user);
	await expect(page.locator('label[for="locale"]')).toHaveText("Sprache:");
	// The dropdown re-syncs to the pinned locale.
	await expect(page.locator("#locale")).toHaveValue("de");
	// The date *values* re-format into the current locale (German: the
	// long weekday, a comma, the day with its period — English has no
	// day period).
	await expect(page.locator(".th-date").first()).toHaveAttribute(
		"title",
		/^([A-ZÄÖÜa-z]+), \d+\./
	);

	// And the answer form is live, on whatever path it was restored.
	await expect(page.locator("#name")).toBeVisible();
	await page.click("td.form-cell >> nth=0 >> .mark-g");
	await expect(page.locator("td.form-cell").first()).toHaveAttribute("data-status", "g");
});
