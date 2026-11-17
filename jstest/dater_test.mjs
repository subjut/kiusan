// dater.js — the meeting form's date/time picker: the calendar, the
// date chips, the per-date times, the slot preview, the submit
// serialization, the timezone prefill, the character counters, and the
// locale re-render.
import test from "node:test";
import assert from "node:assert/strict";
import { boot, settled, click, tick, localTodayIso } from "./harness.mjs";

const enI18N = {
	lang: "en",
	t: {
		"dater.add": "Add",
		"dater.removeDate": "Remove {date}",
		"dater.removeTime": "Remove {time} on {date}",
		"dater.previewEmpty": "No dates selected",
		"dater.previewMidnight": "midnight",
		"dater.previewSummary": "{total} slots ({timed} timed, {midnight} midnight)",
		"dater.previewSummaryNoMidnight": "{total} slots ({timed} timed)",
	},
};
const deI18N = { lang: "de", t: { ...enI18N.t, "dater.add": "Hinzufügen" } };

const pad2 = (n) => String(n).padStart(2, "0");
// A day of the current month / next month, as YYYY-MM-DD (next month is
// always fully in the future; a 20th-of-this-month seed puts past and
// future days into one view).
const thisMonthDay = (d) => {
	const n = new Date();
	return `${n.getFullYear()}-${pad2(n.getMonth() + 1)}-${pad2(d)}`;
};
const nextMonthDay = (d) => {
	const m = new Date(new Date().getFullYear(), new Date().getMonth() + 1, 1);
	return `${m.getFullYear()}-${pad2(m.getMonth() + 1)}-${pad2(d)}`;
};
// The widget's chipDate(): a wall-clock short date in the current locale.
const enShort = (s) => {
	const [y, m, d] = s.split("-").map(Number);
	return new Intl.DateTimeFormat("en-US", {
		day: "numeric", month: "short", year: "numeric",
	}).format(new Date(y, m - 1, d));
};
const timeEn = (h, m) =>
	new Intl.DateTimeFormat("en-US", { hour: "numeric", minute: "2-digit" })
		.format(new Date(2000, 0, 1, h, m));

// The locale <select> lives in the site header — ahead of locale.js, so
// the script finds it while it runs at parse time.
export const LOCALE_SELECT =
	'<select id="locale" data-implicit="en">'+
	'<option value="en" selected>English</option><option value="de">Deutsch</option></select>';

function formFixture({ slots = "", tzOptions = ["America/New_York", "Europe/Berlin", "UTC"], tzSelected = "", localeSelect = false } = {}) {
	const opts = tzOptions.map((z) =>
		`<option value="${z}"${z === tzSelected ? " selected" : ""}>${z.replace(/_/g, " ")}</option>`).join("");
	return `<form id="form" method="POST" action="/meet-form" data-slots="${slots}">
	<input type="text" id="title" name="title" maxlength="100">
	<span class="char-count" id="title-count"></span>
	<input type="text" id="email" name="email" maxlength="256">
	<span class="char-count" id="email-count"></span>
	<textarea id="description" name="description" maxlength="5000"></textarea>
	<span class="char-count" id="description-count"></span>
	<input type="checkbox" id="single-signup" name="single-signup" value="true">
	<p class="form-note" id="single-signup-warn" hidden>wipes</p>
	<input type="date" id="enddate" name="enddate">
	<select id="timezone" name="timezone">${opts}</select>
	<button type="button" id="cal-prev">‹</button>
	<span class="cal-title" id="cal-title"></span>
	<button type="button" id="cal-next">›</button>
	<table class="cal-grid" id="cal-grid"></table>
	<div class="chips" id="date-chips"></div>
	<button type="button" id="clear-dates">clear</button>
	<input type="time" id="time-input">
	<button type="button" id="add-time">all</button>
	<div id="date-times"></div>
	<button type="button" id="clear-times">clear</button>
	<div id="slot-preview"></div>
	<input type="submit" value="Save">
  </form>`;
}

async function page(opts = {}, i18n = enI18N) {
	const w = boot(formFixture(opts), {
		i18n,
		lang: "en-US",
		fetch: opts.fetch,
		head: opts.localeSelect ? LOCALE_SELECT : "",
		headerScripts: ["locale.js"],
		scripts: ["dater.js"],
	});
	await settled(w);
	return w;
}

const doc = (w) => w.document;
const dayBtn = (w, date) => doc(w).querySelector(`button.day[data-date="${date}"]`);
const timeRow = (w, date) =>
	doc(w).querySelector(`.date-time-input[data-date="${date}"]`).closest(".date-times-row");
const rowChips = (w, date) => [...timeRow(w, date).querySelectorAll(".chips .chip")];
const preview = (w) => doc(w).getElementById("slot-preview");

test("the calendar renders the seeded month", async (t) => {
	const seed = nextMonthDay(5);
	const w = await page({ slots: seed });
	const [y, m] = seed.split("-").map(Number);
	assert.equal(doc(w).getElementById("cal-title").textContent,
		new Intl.DateTimeFormat("en-US", { month: "long", year: "numeric" })
			.format(new Date(y, m - 1, 1)));
	const grid = doc(w).getElementById("cal-grid");
	assert.equal(grid.querySelectorAll("tr").length, 7); // weekday header + six weeks
	assert.equal(grid.querySelectorAll("tr:first-child th").length, 7);
	const days = grid.querySelectorAll("button.day");
	const dim = new Date(y, m, 0).getDate();
	assert.equal(days.length, dim);
	assert.equal(days[0].getAttribute("data-date"), `${y}-${pad2(m)}-01`);
	assert.equal(days[days.length - 1].getAttribute("data-date"), `${y}-${pad2(m)}-${pad2(dim)}`);
	for (const b of days) assert.ok(!b.disabled); // the whole month is in the future
});

test("selecting and deselecting a date", async (t) => {
	const d = nextMonthDay(10);
	const w = await page({ slots: nextMonthDay(5) });
	const seeded = nextMonthDay(5);
	click(w, dayBtn(w, d));
	// the click re-renders the grid: re-query the (new) button
	assert.ok(dayBtn(w, d).classList.contains("selected"));
	const chips = () => [...doc(w).querySelectorAll("#date-chips .chip")];
	assert.equal(chips().length, 2); // the seeded day + the selected one
	const chip = chips().find((c) => c.textContent.includes(enShort(d)));
	assert.ok(chip);
	assert.equal(chip.querySelector(".chip-x").getAttribute("data-date"), d);
	assert.equal(chip.querySelector(".chip-x").getAttribute("title"), `Remove ${enShort(d)}`);
	const row = timeRow(w, d);
	assert.equal(row.querySelector(".date-times-label").textContent, enShort(d));
	assert.equal(row.querySelector(".date-time-add").textContent, "Add");
	assert.ok(preview(w).textContent.includes(enShort(d)));
	assert.ok(preview(w).textContent.includes("midnight"));
	assert.ok(preview(w).textContent.includes("2 slots (0 timed, 2 midnight)"));
	// deselect
	click(w, dayBtn(w, d));
	assert.ok(!dayBtn(w, d).classList.contains("selected"));
	assert.equal(chips().length, 1); // back to just the seeded day
	assert.equal(doc(w).querySelectorAll("#date-times .date-times-row").length, 1);
	assert.ok(preview(w).textContent.includes(enShort(seeded)));
});

test("past days are disabled and unclickable", async (t) => {
	const w = await page({ slots: thisMonthDay(20) });
	const min = localTodayIso();
	const days = [...doc(w).querySelectorAll("#cal-grid button.day")];
	assert.ok(days.length > 0);
	for (const b of days)
		assert.equal(b.disabled, b.getAttribute("data-date") < min,
			b.getAttribute("data-date"));
	const past = days.find((b) => b.disabled);
	if (past) {
		// a click on a disabled day changes nothing (the seeded date's
		// chip stays the only one)
		const before = doc(w).querySelectorAll("#date-chips .chip").length;
		click(w, past);
		assert.equal(doc(w).querySelectorAll("#date-chips .chip").length, before);
		assert.ok(!past.classList.contains("selected"));
	}
});

test("a time can be added to all selected dates", async (t) => {
	const d1 = nextMonthDay(3), d2 = nextMonthDay(9);
	const w = await page({ slots: d1 + "," + d2 });
	doc(w).getElementById("time-input").value = "14:30";
	click(w, doc(w).getElementById("add-time"));
	assert.equal(doc(w).getElementById("time-input").value, "");
	for (const d of [d1, d2]) {
		const chips = rowChips(w, d);
		assert.equal(chips.length, 1);
		assert.ok(chips[0].textContent.includes(timeEn(14, 30)));
		assert.equal(chips[0].querySelector(".chip-x").getAttribute("data-time"), "14:30");
		assert.equal(chips[0].querySelector(".chip-x").getAttribute("title"),
			`Remove ${timeEn(14, 30)} on ${enShort(d)}`);
	}
	assert.ok(preview(w).textContent.includes("2 slots (2 timed)"));
});

test("a per-date time is added with its Add button and Enter", async (t) => {
	const d = nextMonthDay(5);
	const w = await page({ slots: d });
	timeRow(w, d).querySelector(".date-time-input").value = "09:00";
	click(w, timeRow(w, d).querySelector(".date-time-add"));
	assert.equal(rowChips(w, d).length, 1);
	assert.ok(rowChips(w, d)[0].textContent.includes(timeEn(9, 0)));
	// Enter in the input adds the same way (the add re-rendered the row)
	const input = timeRow(w, d).querySelector(".date-time-input");
	input.value = "17:45";
	input.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
	assert.equal(rowChips(w, d).length, 2);
	assert.ok(rowChips(w, d)[1].textContent.includes(timeEn(17, 45)));
});

test("the chip remove buttons drop times and dates", async (t) => {
	const d1 = nextMonthDay(3), d2 = nextMonthDay(9);
	const w = await page({ slots: d1 + "," + d2 });
	doc(w).getElementById("time-input").value = "10:00";
	click(w, doc(w).getElementById("add-time"));
	// remove the time of d1 only
	click(w, rowChips(w, d1)[0].querySelector(".chip-x"));
	assert.equal(rowChips(w, d1).length, 0);
	assert.equal(rowChips(w, d2).length, 1);
	assert.equal(doc(w).querySelectorAll("#date-chips .chip").length, 2); // dates stay
	// remove the date d1 itself
	const chip1 = [...doc(w).querySelectorAll("#date-chips .chip")]
		.find((c) => c.textContent.includes(enShort(d1)));
	click(w, chip1.querySelector(".chip-x"));
	assert.equal(doc(w).querySelectorAll("#date-chips .chip").length, 1);
	assert.ok(!doc(w).querySelector(`.date-time-input[data-date="${d1}"]`));
});

test("clear-times keeps the dates; clear-dates clears everything", async (t) => {
	const d1 = nextMonthDay(3), d2 = nextMonthDay(9);
	const w = await page({ slots: d1 + "," + d2 });
	doc(w).getElementById("time-input").value = "10:00";
	click(w, doc(w).getElementById("add-time"));
	click(w, doc(w).getElementById("clear-times"));
	assert.equal(doc(w).querySelectorAll("#date-chips .chip").length, 2);
	assert.equal(doc(w).querySelectorAll("#date-times .date-times-row").length, 2);
	assert.equal(doc(w).querySelectorAll("#date-times .chips .chip").length, 0);
	assert.ok(preview(w).textContent.includes("2 slots (0 timed, 2 midnight)"));
	click(w, doc(w).getElementById("clear-dates"));
	assert.equal(doc(w).querySelectorAll("#date-chips .chip").length, 0);
	assert.equal(doc(w).querySelectorAll("#date-times .date-times-row").length, 0);
	assert.equal(preview(w).textContent, "No dates selected");
});

test("the submit serializes the slots", async (t) => {
	const d1 = nextMonthDay(3), d2 = nextMonthDay(9);
	const w = await page({ slots: d1 + "," + d2 });
	timeRow(w, d2).querySelector(".date-time-input").value = "14:30";
	click(w, timeRow(w, d2).querySelector(".date-time-add"));
	// the add re-rendered the row: re-query before the second time
	timeRow(w, d2).querySelector(".date-time-input").value = "09:00";
	click(w, timeRow(w, d2).querySelector(".date-time-add"));
	const form = doc(w).getElementById("form");
	form.dispatchEvent(new w.Event("submit", { bubbles: true, cancelable: true }));
	let inputs = [...doc(w).querySelectorAll("#form [data-picker-input]")];
	assert.deepEqual(inputs.map((i) => i.value),
		[d1, `${d2}T09:00`, `${d2}T14:30`]);
	assert.deepEqual(inputs.map((i) => i.name), ["slot0", "slot1", "slot2"]);
	for (const i of inputs)
		assert.equal(i.type, "hidden");
	// resubmitting replaces, not appends
	form.dispatchEvent(new w.Event("submit", { bubbles: true, cancelable: true }));
	inputs = [...doc(w).querySelectorAll("#form [data-picker-input]")];
	assert.equal(inputs.length, 3);
});

test("seeded slots prefill the picker (edit mode)", async (t) => {
	const d1 = nextMonthDay(5), d2 = nextMonthDay(6);
	const w = await page({ slots: `${d1}T10:00,${d2},${d1}T09:00` });
	assert.equal(doc(w).querySelectorAll("#date-chips .chip").length, 2);
	assert.equal(doc(w).querySelectorAll("#date-times .date-times-row").length, 2);
	// the chip's text node (the trailing ✕ button stays out)
	const chips = rowChips(w, d1).map((c) => c.firstChild.textContent);
	assert.deepEqual(chips, [timeEn(9, 0), timeEn(10, 0)]);
	assert.equal(rowChips(w, d2).length, 0);
	assert.ok(dayBtn(w, d1).classList.contains("selected"));
	assert.ok(dayBtn(w, d2).classList.contains("selected"));
	assert.ok(preview(w).textContent.includes(`${timeEn(9, 0)}, ${timeEn(10, 0)}`));
	assert.ok(preview(w).textContent.includes("3 slots (2 timed, 1 midnight)"));
});

test("the browser's timezone is prefilled in the select", async (t) => {
	const w = await page({});
	const sel = doc(w).getElementById("timezone");
	const localTz = (w.Intl ?? Intl).DateTimeFormat().resolvedOptions().timeZone;
	assert.equal(sel.value, localTz);
	// the option list stays sorted, and an inserted zone shows spaces
	const vals = [...sel.options].map((o) => o.value);
	assert.deepEqual(vals, [...vals].sort());
	if (!["America/New_York", "Europe/Berlin", "UTC"].includes(localTz)) {
		const opt = [...sel.options].find((o) => o.value === localTz);
		assert.equal(opt.textContent, localTz.replace(/_/g, " "));
	}
});

test("edit mode keeps the server-selected zone", async (t) => {
	const w = await page({ tzSelected: "Europe/Berlin" });
	const sel = doc(w).getElementById("timezone");
	assert.equal(sel.value, "Europe/Berlin");
	assert.equal(sel.options.length, 3); // no prefill insertion
});

test("character counters count code points", async (t) => {
	const w = await page({});
	const el = doc(w).getElementById("title");
	const label = doc(w).getElementById("title-count");
	assert.equal(label.textContent, "0 / 100");
	el.value = "Hello";
	el.dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.equal(label.textContent, "5 / 100");
	el.value = "👍👍"; // one code point each, like the server's rune count
	el.dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.equal(label.textContent, "2 / 100");
});

test("the single-signup wipe warning follows the checkbox", async (t) => {
	const w = await page({});
	const ss = doc(w).getElementById("single-signup");
	const warn = doc(w).getElementById("single-signup-warn");
	assert.ok(warn.hidden);
	ss.checked = true;
	ss.dispatchEvent(new w.Event("change", { bubbles: true }));
	assert.ok(!warn.hidden);
	ss.checked = false;
	ss.dispatchEvent(new w.Event("change", { bubbles: true }));
	assert.ok(warn.hidden);
});

test("a locale switch re-renders the picker", async (t) => {
	const d = nextMonthDay(5);
	const w = boot(formFixture({ slots: d }), {
		i18n: enI18N,
		lang: "en-US",
		fetch: (u) => Promise.resolve({ ok: true, json: () => Promise.resolve(deI18N) }),
		head: LOCALE_SELECT,
		headerScripts: ["locale.js"],
		scripts: ["dater.js"],
	});
	await settled(w);
	const [y, m] = d.split("-").map(Number);
	const deTitle = new Intl.DateTimeFormat("de-DE", { month: "long", year: "numeric" })
		.format(new Date(y, m - 1, 1));
	assert.notEqual(doc(w).getElementById("cal-title").textContent, deTitle);
	doc(w).getElementById("locale").value = "de";
	doc(w).getElementById("locale").dispatchEvent(new w.Event("change"));
	await tick();
	assert.equal(doc(w).getElementById("cal-title").textContent, deTitle);
	// weekday headers follow the locale's narrow names
	// Sunday … Saturday, de-DE narrow names
	assert.deepEqual(
		[...doc(w).querySelectorAll("#cal-grid tr:first-child th")].map((th) => th.textContent),
		["S", "M", "D", "M", "D", "F", "S"]);
	// and the widget's own strings come from the refreshed dictionary
	assert.ok(doc(w).querySelector("#date-times .date-time-add").textContent.includes("Hinzufügen"));
	assert.ok(w.document.cookie.includes("kiusan-locale=de"));
});
