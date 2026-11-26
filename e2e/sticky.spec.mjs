// The sticky table header on a real table: page.js measures the thead
// row heights into the --th-N vars (and pins .table-sticky), and the
// .table-scroll region scrolls the table in place. jsdom reports zero
// heights — the jsdom suite can only pin the hardcoded 0px offsets — so
// the real measured offsets and the actual pinning can only be pinned
// here, against the seeded max-size meeting (100 slots x 100
// respondents: both dimensions overflow the region).
import { test, expect } from "@playwright/test";
import { seed } from "./env.mjs";

const px = (s) => parseFloat(s);

test("a max-size table pins its measured header and scrolls in its region", async ({ page }) => {
	const t = seed("meeting 100x100");
	await page.goto(t.admin);

	const table = page.locator(".answers-table");
	// The pinning activates only once the offsets are measured.
	await expect(table).toHaveClass(/table-sticky/);

	// The measured cumulative offsets: row 1 pins at 0, row 2 at row 1's
	// height, row 3 at row 1 + 2 — non-zero in a real layout (jsdom:
	// everything 0px).
	const vars = await table.evaluate((el) =>
		[1, 2, 3].map((i) => el.style.getPropertyValue("--th-" + i))
	);
	expect(vars[0]).toBe("0px");
	expect(px(vars[1])).toBeGreaterThan(0);
	expect(px(vars[2])).toBeGreaterThan(px(vars[1]));

	const region = page.locator(".table-scroll");
	// The region is the scroller in both axes (100 respondents tall,
	// 100 slots wide).
	const overflow = await region.evaluate((el) => ({
		v: el.scrollHeight > el.clientHeight,
		h: el.scrollWidth > el.clientWidth,
	}));
	expect(overflow.v).toBe(true);
	expect(overflow.h).toBe(true);

	// Scroll past the header: every row pins at its measured offset, and
	// the first data row leaves the region.
	await region.evaluate((el) => {
		el.scrollTop = 300;
	});
	const tops = await table.evaluate((el) => {
		const regionTop = el.closest(".table-scroll").getBoundingClientRect().top;
		return [...el.tHead.rows].map((tr) =>
			tr.firstElementChild.getBoundingClientRect().top - regionTop
		);
	});
	expect(tops[0]).toBeCloseTo(px(vars[0]), 0);
	expect(tops[1]).toBeCloseTo(px(vars[1]), 0);
	expect(tops[2]).toBeCloseTo(px(vars[2]), 0);

	// The first data row (table.rows would hand back the thead's first
	// row): with the header pinned, it sits above the region's top once
	// the table has scrolled (the header is well under the 300px scroll).
	const firstData = await table.evaluate((el) =>
		el.tBodies[0].rows[0].firstElementChild.getBoundingClientRect().top -
		el.closest(".table-scroll").getBoundingClientRect().top
	);
	expect(firstData).toBeLessThan(0);
});
