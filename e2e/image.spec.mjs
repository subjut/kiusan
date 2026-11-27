// Image options in a real browser: the upload (file picker -> the server
// resizes, re-encodes, and stores it, the row keeps a 64px thumbnail and
// the stored image's ID), the row order becoming the column order, the
// 64px thumbnail under the label in the answers table, and the lightbox
// (full size over the greyed backdrop, Esc and the backdrop close it;
// a single-image vote's prev/next arrows stay hidden).
import { test, expect } from "@playwright/test";

// A 64x64 solid red PNG (the only binary this suite carries: every
// server-side step — decode, resize, JPEG re-encode, store, serve — is
// exercised through it).
const PNG = Buffer.from(
	"iVBORw0KGgoAAAANSUhEUgAAAEAAAABACAYAAACqaXHeAAAAl0lEQVR4nO3QMREAIAzAwMqpfz14ARk/kOH3XObs3p+NDtAaoAO0BugArQE6QGuADtAaoAO0BugArQE6QGuADtAaoAO0BugArQE6QGuADtAaoAO0BugArQE6QGuADtAaoAO0BugArQE6QGuADtAaoAO0BugArQE6QGuADtAaoAO0BugArQE6QGuADtAaoAO0BugArQE6QHtG7LH/UgR9aQAAAABJRU5ErkJggg==",
	"base64"
);

test("upload -> reorder -> table display -> lightbox", async ({ page }) => {
	// ---- The organizer creates the image-option vote through the form ----
	await page.goto("/vote-form");
	await page.fill("#title", "E2E image vote");
	await page.fill("#email", "img@kiusan.test");

	const row0 = page.locator("#choice-rows .choice-row").nth(0);
	// The file picker of the first row: the server resizes and stores the
	// image, the row shows the 64px thumbnail and carries the ID.
	await row0
		.locator(".choice-file")
		.setInputFiles({ name: "pic.png", mimeType: "image/png", buffer: PNG });
	const thumb = row0.locator(".choice-slot img.choice-img");
	await expect(thumb).toHaveAttribute("src", /^\/vote-img\/[^/]+\/thumb$/);
	await expect(row0.locator(".choice-slot")).toHaveClass(/filled/);

	// Label the option and add a text-only one after it.
	await row0.locator('input[type=text]').fill("Red square");
	await page.click("#add-choice");
	const row1 = page.locator("#choice-rows .choice-row").nth(1);
	await row1.locator('input[type=text]').fill("Plain option");

	// Reorder the text option in front: the row order is the column order.
	await row1.locator('.choice-move[data-dir="up"]').click();
	await expect(
		page.locator("#choice-rows .choice-row input[type=text]").nth(0)
	).toHaveValue("Plain option");

	await page.locator("#form input[type=submit]").click();
	await expect(page).toHaveURL(/\/vote\/[^/]+$/);

	// The admin page: the first column is the text option, the second the
	// image (the label above the 64px thumbnail).
	const ths = page.locator("thead th.th-choice");
	await expect(ths).toHaveCount(2);
	await expect(ths.nth(0)).toContainText("Plain option");
	await expect(ths.nth(1)).toHaveClass(/th-img/);
	await expect(ths.nth(1)).toContainText("Red square");
	await expect(ths.nth(1).locator(".choice-thumb img")).toHaveAttribute(
		"src",
		/^\/vote-img\/[^/]+\/thumb$/
	);

	// ---- The lightbox: full size over the greyed backdrop ----
	const box = page.locator("#lightbox");
	await expect(box).toBeHidden();
	await ths.nth(1).locator(".choice-thumb").click();
	await expect(box).toBeVisible();
	// This vote carries one image option — nothing to cycle to, so the
	// prev/next arrows stay hidden (a moving target for nothing).
	await expect(box.locator(".lightbox-prev")).toBeHidden();
	await expect(box.locator(".lightbox-next")).toBeHidden();
	const full = box.locator(".lightbox-img");
	await expect(full).toHaveAttribute("src", /^\/vote-img\/[^/]+$/);
	await expect(full).not.toHaveAttribute("src", /thumb$/);
	await expect(full).toBeVisible();
	// Esc closes it.
	await page.keyboard.press("Escape");
	await expect(box).toBeHidden();

	// A second open: the backdrop click closes it (a corner of the box,
	// not the centered image).
	await ths.nth(1).locator(".choice-thumb").click();
	await expect(box).toBeVisible();
	await box.click({ position: { x: 4, y: 4 } });
	await expect(box).toBeHidden();

	// ---- The served image: both sizes resolve as immutable JPEGs ----
	const src = await ths.nth(1).locator(".choice-thumb").getAttribute("data-src");
	for (const [suffix, path] of [["full", src], ["thumb", src + "/thumb"]]) {
		const res = await page.request.get(path);
		expect(res.status(), suffix).toBe(200);
		expect(res.headers()["content-type"], suffix).toBe("image/jpeg");
		expect(res.headers()["cache-control"], suffix).toContain("immutable");
	}
});
