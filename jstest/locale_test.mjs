// locale.js — locale resolution (cookie → browser language), the rewrite
// of server-rendered date values, the UTC-offset annotation, and the
// dropdown's dictionary swap.
import test from "node:test";
import assert from "node:assert/strict";
import { boot, settled, tick } from "./harness.mjs";

// A slice of the meeting page: the container carrying the poll's zone
// (data-tz), the timezone line, the expiry date, and the year / date /
// time header cells (all November 2026 Saturdays, 11:00 Europe/Berlin).
function pageFixture(tz = "Europe/Berlin") {
	return `<div class="container" data-tz="${tz}">
	<p class="meta"><span class="tz-name" data-base="${tz}">${tz}</span>
	  <span class="f-date" data-slot="2026-11-07T10:00Z" data-style="medium">Nov 7, 2026</span>
	  <span class="f-date" data-slot="2026-11-07T10:00Z" data-style="datetime">7 Nov 2026 11:00</span></p>
	<table class="answers-table"><thead>
	  <tr><th class="th-year" data-slot="2026-11-07T10:00Z" colspan="3">2026</th></tr>
	  <tr>
	    <th class="th-date" data-slot="2026-11-07T10:00Z" data-lines="1" title="x">7 Nov Sat</th>
	    <th class="th-date" data-slot="2026-11-14T10:00Z" data-lines="2" title="x">14 Nov Sat</th>
	    <th class="th-date" data-slot="2026-11-21T10:00Z" data-lines="3" title="x">21 Nov Sat</th>
	  </tr>
	  <tr>
	    <th class="th-time" data-slot="2026-11-07T10:00Z">12:00</th>
	    <th class="th-time" data-slot="2026-11-14T10:00Z">12:00</th>
	    <th class="th-time">—</th>
	  </tr>
	</thead></table>
  </div>`;
}

async function page(opts) {
	const w = boot(pageFixture(opts.tz),
		{ lang: opts.lang, cookies: opts.cookies, headerScripts: ["locale.js"] });
	await settled(w);
	return w;
}

const q = (w, sel) => w.document.querySelector(sel);

// The widget appends the poll's long offset (en-US longOffset) to the zone
// name; the exact string is ICU-version dependent ("GMT+1" in older ICU,
// "GMT+01:00" in newer), so compute the expectation the same way.
const longOffset = (tz) => new Intl.DateTimeFormat("en-US",
	{ timeZoneName: "longOffset", timeZone: tz })
	.formatToParts(new Date()).find((p) => p.type === "timeZoneName").value;

test("date values render in the browser locale (en-US)", async (t) => {
	const w = await page({ lang: "en-US" });
	assert.equal(w.Locale.get(), "en-US");
	assert.equal(q(w, ".th-year").textContent, "2026");
	assert.equal(q(w, '.th-date[data-lines="1"]').textContent, "7 Nov Sat");
	assert.equal(q(w, '.th-date[data-lines="2"]').textContent, "14 Nov\nSat");
	assert.equal(q(w, '.th-date[data-lines="3"]').textContent, "21\nNov\nSat");
	assert.equal(q(w, '.th-date[data-lines="1"]').title, "Saturday, November 7, 2026");
	assert.equal(q(w, ".th-time").textContent, "11:00 AM");
	assert.equal(q(w, ".f-date").textContent, "Nov 7, 2026");
	// The datetime style carries the clock, in the poll's zone (10:00Z =
	// 11:00 in November Berlin).
	assert.equal(q(w, '.f-date[data-style="datetime"]').textContent, "Nov 7, 2026, 11:00 AM");
	assert.equal(q(w, ".tz-name").textContent, `Europe/Berlin (${longOffset("Europe/Berlin")})`);
});

test("the kiusan-locale cookie beats the browser language", async (t) => {
	const w = await page({ lang: "fr-FR", cookies: { "kiusan-locale": "de" } });
	assert.equal(w.Locale.get(), "de");
	assert.equal(q(w, '.th-date[data-lines="1"]').textContent, "7 Nov Sa");
	assert.equal(q(w, '.th-date[data-lines="2"]').textContent, "14 Nov\nSa");
	assert.equal(q(w, '.th-date[data-lines="1"]').title, "Samstag, 7. November 2026");
	assert.equal(q(w, ".th-time").textContent, "11:00");
	assert.equal(q(w, ".f-date").textContent, "7. Nov. 2026");
	assert.equal(q(w, '.f-date[data-style="datetime"]').textContent, "7. Nov. 2026, 11:00");
	assert.equal(q(w, ".tz-name").textContent, `Europe/Berlin (${longOffset("Europe/Berlin")})`);
});

test("an invalid cookie falls back to the browser language", async (t) => {
	const w = await page({ lang: "en-US", cookies: { "kiusan-locale": "not a locale" } });
	assert.equal(w.Locale.get(), "en-US");
	assert.equal(q(w, '.th-date[data-lines="1"]').textContent, "7 Nov Sat");
});

test("a poll without a zone renders in UTC", async (t) => {
	const w = await page({ tz: "UTC", lang: "en-US" });
	assert.equal(q(w, ".tz-name").textContent, `UTC (${longOffset("UTC")})`);
});

test("a page without a poll zone is left alone", async (t) => {
	// The form page has no [data-tz] container: the server-rendered
	// labels stay as the no-JS fallback.
	const body = pageFixture("UTC").replace(
		`<div class="container" data-tz="UTC">`, "<div>");
	const w = boot(body, { lang: "de", headerScripts: ["locale.js"] });
	await settled(w);
	assert.equal(q(w, ".th-date[data-lines='1']").textContent, "7 Nov Sat");
	assert.equal(q(w, ".f-date").textContent, "Nov 7, 2026");
	assert.equal(q(w, ".tz-name").textContent, "UTC");
});

function dictFixture() {
	return pageFixture("UTC") + `
	<p data-i18n="k1">Create</p>
	<p data-i18n="k2">Untouched</p>
	<input data-i18n-ph="ph1" placeholder="Your name">
	<button data-i18n-title="t1" title="Copy">C</button>
	<a data-i18n-aria="a1" aria-label="Copy">C</a>
	<meta data-i18n-meta="m1" content="A meeting poll">
	<h1 data-i18n-prefix="p1" data-suffix=" Dinner">Create: Dinner</h1>`;
}

// The locale <select> lives in the site header — ahead of locale.js, so
// the script finds it while it runs at parse time.
// data-implicit is the language the server would serve without a cookie
// (Accept-Language resolved onto a shipped language, English while none
// matches) — the pick it compares against for the pin/un-pin decision.
const LOCALE_SELECT =
	'<select id="locale" data-implicit="en">'+
	'<option value="en">English</option><option value="de">Deutsch</option></select>';

const deDict = {
	lang: "de",
	t: {
		k1: "Erstellen",
		ph1: "Dein Name",
		t1: "Kopieren",
		a1: "Kopieren",
		m1: "Eine Abstimmung",
		p1: "Erstellen:",
	},
};

test("the dropdown fetches the dictionary and swaps the marked nodes", async (t) => {
	const urls = [];
	const w = boot(dictFixture(), {
		lang: "en-US",
		fetch: (u) => {
			urls.push(u);
			return Promise.resolve({ ok: true, json: () => Promise.resolve(deDict) });
		},
		head: LOCALE_SELECT,
		headerScripts: ["locale.js"],
	});
	await settled(w);
	let changes = 0;
	w.Locale.onChange(() => changes++);
	w.document.getElementById("locale").value = "de";
	w.document.getElementById("locale").dispatchEvent(new w.Event("change"));
	await tick();

	assert.deepEqual(urls, ["/i18n/de"]);
	assert.ok(w.document.cookie.includes("kiusan-locale=de"));
	assert.equal(w.I18N, deDict);
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Erstellen");
	assert.equal(q(w, "[data-i18n=k2]").textContent, "Untouched"); // missing key stays
	assert.equal(q(w, "[data-i18n-ph=ph1]").placeholder, "Dein Name");
	assert.equal(q(w, "[data-i18n-title=t1]").getAttribute("title"), "Kopieren");
	assert.equal(q(w, "[data-i18n-aria=a1]").getAttribute("aria-label"), "Kopieren");
	assert.equal(q(w, "[data-i18n-meta=m1]").getAttribute("content"), "Eine Abstimmung");
	assert.equal(q(w, "[data-i18n-prefix=p1]").textContent, "Erstellen: Dinner"); // keeps data-suffix
	assert.equal(w.document.documentElement.lang, "de");
	// the date values followed the switch
	assert.equal(q(w, '.th-date[data-lines="1"]').textContent, "7 Nov Sa");
	assert.equal(changes, 1);
});

test("the select shows the effective language on load", async (t) => {
	const head = '<select id="locale" data-implicit="de">'+
		'<option value="en">English</option><option value="de" selected>Deutsch</option></select>';
	// a cookie pin beats the browser language
	let w = boot(dictFixture(), {
		lang: "fr-FR", cookies: { "kiusan-locale": "de" },
		head: head, headerScripts: ["locale.js"],
	});
	await settled(w);
	assert.equal(w.document.getElementById("locale").value, "de");
	// without a cookie, the browser language matches a shipped option
	w = boot(dictFixture(), { lang: "de-DE", head: head, headerScripts: ["locale.js"] });
	await settled(w);
	assert.equal(w.document.getElementById("locale").value, "de");
	// an unshipped browser language falls back to English
	w = boot(dictFixture(), { lang: "fr-FR", head: head, headerScripts: ["locale.js"] });
	await settled(w);
	assert.equal(w.document.getElementById("locale").value, "en");
});

test("a pick of the implicit language un-pins (deletes the cookie)", async (t) => {
	const urls = [];
	const w = boot(dictFixture(), {
		lang: "fr-FR",
		fetch: (u) => {
			urls.push(u);
			return Promise.resolve({ ok: true, json: () => Promise.resolve(deDict) });
		},
		head: LOCALE_SELECT, // data-implicit="en": a French browser
		headerScripts: ["locale.js"],
	});
	await settled(w);
	const sel = w.document.getElementById("locale");
	// a deviation pins
	sel.value = "de";
	sel.dispatchEvent(new w.Event("change"));
	await tick();
	assert.ok(w.document.cookie.includes("kiusan-locale=de"));
	// back to the implicit language: the pin is deleted
	sel.value = "en";
	sel.dispatchEvent(new w.Event("change"));
	await tick();
	assert.ok(!w.document.cookie.includes("kiusan-locale"));
	assert.deepEqual(urls, ["/i18n/de", "/i18n/en"]);
});

test("a failed dictionary fetch leaves the page as-is", async (t) => {
	const w = boot(dictFixture(), {
		lang: "en-US",
		fetch: () => Promise.resolve({ ok: false, status: 404 }),
		head: LOCALE_SELECT,
		headerScripts: ["locale.js"],
	});
	await settled(w);
	w.document.getElementById("locale").value = "de";
	w.document.getElementById("locale").dispatchEvent(new w.Event("change"));
	await tick();
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Create");
	assert.equal(w.document.documentElement.lang, "en");
});

// Restore-mode models for the pageshow reconciliation: window.I18N is
// seeded the way /i18n/boot seeds it (a page always has it; it carries
// the locale the body currently reflects, and the cookie the dropdown
// writes).
const enDict = {
	lang: "en",
	t: {
		k1: "Create",
		ph1: "Your name",
		t1: "Copy",
		a1: "Copy",
		m1: "A meeting poll",
		p1: "Create:",
	},
};

// A back/forward-cache hit: the parked document as-is, scripts not
// re-executing — body and window.I18N alike are from before the switch.
function restoreFromBfcache(w) {
	const ev = new w.Event("pageshow");
	ev.persisted = true;
	w.dispatchEvent(ev);
}

test("a bfcache restore re-applies a locale changed on a later page", async (t) => {
	const urls = [];
	const w = boot(dictFixture(), {
		lang: "en-US",
		i18n: enDict, // the parked dictionary: the page was showing English
		fetch: (u) => {
			urls.push(u);
			return Promise.resolve({ ok: true, json: () => Promise.resolve(deDict) });
		},
		head: LOCALE_SELECT,
		headerScripts: ["locale.js"],
	});
	await settled(w);
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Create"); // the parked page is English
	w.document.cookie = "kiusan-locale=de; path=/"; // set elsewhere (on page B's dropdown)
	let changes = 0;
	w.Locale.onChange(() => changes++);
	restoreFromBfcache(w);
	await tick();

	assert.deepEqual(urls, ["/i18n/de"]);
	assert.equal(w.I18N, deDict);
	assert.equal(w.document.getElementById("locale").value, "de");
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Erstellen");
	assert.equal(w.document.documentElement.lang, "de");
	// The date values followed the switch, and the widgets were told.
	assert.equal(q(w, '.th-date[data-lines="1"]').textContent, "7 Nov Sa");
	assert.equal(changes, 1);
});

test("a bfcache restore without a locale change never re-fetches", async (t) => {
	const urls = [];
	const w = boot(dictFixture(), {
		lang: "en-US",
		cookies: { "kiusan-locale": "en" },
		i18n: enDict,
		fetch: (u) => {
			urls.push(u);
			return Promise.resolve({ ok: true, json: () => Promise.resolve(deDict) });
		},
		head: LOCALE_SELECT,
		headerScripts: ["locale.js"],
	});
	await settled(w);
	restoreFromBfcache(w);
	await tick();

	assert.deepEqual(urls, []);
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Create");
	assert.equal(w.document.documentElement.lang, "en");
	assert.equal(w.document.getElementById("locale").value, "en");
});

test("a fresh load's pageshow (current dictionary, up-to-date body) never re-fetches", async (t) => {
	// The re-apply arm is an idempotent no-op on a fresh render — server
	// and client resolved the same cookie, so body and dictionary agree.
	const urls = [];
	const w = boot(dictFixture(), {
		lang: "en-US",
		i18n: enDict,
		fetch: (u) => {
			urls.push(u);
			return Promise.resolve({ ok: true, json: () => Promise.resolve(deDict) });
		},
		head: LOCALE_SELECT,
		headerScripts: ["locale.js"],
	});
	await settled(w);
	w.dispatchEvent(new w.Event("pageshow")); // persisted stays false
	await tick();
	assert.deepEqual(urls, []);
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Create");
	assert.equal(w.document.getElementById("locale").value, "en");
});

// The home-page case: the first page of a session is the bfcache entry
// most likely to be evicted, so it comes back through a re-parse. The
// scripts re-run — /i18n/boot (no-cache) re-fetches, so window.I18N is
// already the current dictionary, and the dropdown re-syncs to the
// cookie — while the body's data-i18n strings are still the stale
// first-render ones. The current dictionary must be re-applied, with no
// fetch.
test("a re-parsed restore re-applies the current dictionary to the stale body", async (t) => {
	const urls = [];
	const w = boot(dictFixture(), {
		lang: "en-US",
		cookies: { "kiusan-locale": "de" },
		i18n: deDict, // boot re-fetched it for the current cookie
		fetch: (u) => {
			urls.push(u);
			return Promise.resolve({ ok: true, json: () => Promise.resolve(deDict) });
		},
		head: LOCALE_SELECT,
		headerScripts: ["locale.js"],
	});
	await settled(w);
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Create"); // the stale body is English
	w.dispatchEvent(new w.Event("pageshow")); // persisted stays false
	await tick();

	assert.deepEqual(urls, []);
	assert.equal(w.document.getElementById("locale").value, "de");
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Erstellen");
	assert.equal(w.document.documentElement.lang, "de");
});

test("a failed dictionary fetch on a restore leaves the page as-is", async (t) => {
	const w = boot(dictFixture(), {
		lang: "en-US",
		i18n: enDict, // parked in English...
		fetch: () => Promise.resolve({ ok: false, status: 404 }),
		head: LOCALE_SELECT,
		headerScripts: ["locale.js"],
	});
	await settled(w);
	w.document.cookie = "kiusan-locale=de; path=/"; // ...but the cookie changed
	restoreFromBfcache(w);
	await tick();
	assert.equal(q(w, "[data-i18n=k1]").textContent, "Create");
	assert.equal(w.document.documentElement.lang, "en");
});
