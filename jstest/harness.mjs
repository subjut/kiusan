// The test harness for static/js/*: the real scripts are injected as inline
// <script> elements the way the pages load them — locale.js from the site
// header (before the body), page.js ahead of poll.js / vote.js at the
// bottom of the page, dater.js on the form page — so the tests exercise
// the shipped code against DOM fixtures that mirror the template markup.
//
// The scripts run at parse time, exactly like in the browser: the
// cookie / navigator / I18N seeds are inline scripts in the head, ahead
// of the page scripts. What jsdom cannot provide, the scripts already
// guard against (no ResizeObserver, zero-sized rects, no layout); jsdom's
// "not implemented" console noise (navigation etc.) is silenced via the
// empty VirtualConsole.
//
// (Scripts must be loaded as <script> elements, not via window.eval: on
// this Node build an eval'd script gets a fresh realm whose globals don't
// reflect the jsdom window.)

import { JSDOM, VirtualConsole } from "jsdom";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const jsDir = path.resolve(
	path.dirname(fileURLToPath(import.meta.url)),
	"..", "static", "js");

export function script(name) {
	return readFileSync(path.join(jsDir, name), "utf8");
}

// boot: a fresh page from the fixture body.
//   head   — markup injected into <head>, ahead of the header scripts
//                    (like the locale <select> in the site header)
//   headerScripts — scripts injected into <head>, before any of the
//                    body (like locale.js from the site header)
//   scripts       — scripts injected at the end of <body> (like page.js
//                    ahead of poll.js / vote.js, or dater.js)
//   lang   — navigator.language (the "browser" locale)
//   cookies — server cookies to seed, "name" -> value
//   i18n   — the /i18n/boot payload (window.I18N)
//   fetch  — a stub for the /i18n/:lang dictionary fetch
export function boot(body, {
	url = "https://kiusan.test/",
	lang,
	cookies = {},
	i18n,
	fetch: fetchStub,
	head = "",
	headerScripts = [],
	scripts: tailScripts = [],
} = {}) {
	// The seeds run before any page script: locale.js reads the cookie and
	// navigator.language at parse time, dater.js reads window.I18N.
	const seeds = [];
	for (const [k, v] of Object.entries(cookies))
		seeds.push(`document.cookie = ${JSON.stringify(k + "=" + v + "; path=/")};`);
	if (lang !== undefined)
		seeds.push(`Object.defineProperty(navigator, "language", { value: ${JSON.stringify(lang)}, configurable: true });`);
	if (i18n)
		seeds.push(`window.I18N = ${JSON.stringify(i18n)};`);
	const inline = (names) =>
		names.map((n) => `<script>${script(n)}</script>`).join("");
	const html = `<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">
	${seeds.map((s) => `<script>${s}</script>`).join("")}${head}${inline(headerScripts)}</head><body>${body}${inline(tailScripts)}</body></html>`;
	const window = new JSDOM(html, {
		url,
		pretendToBeVisual: true,
		runScripts: "dangerously",
		virtualConsole: new VirtualConsole(),
	}).window;
	if (!window.Intl) window.Intl = Intl; // fall back to Node's Intl
	if (fetchStub) window.fetch = fetchStub;
	return window;
}

// settled: resolves once the document has finished loading — the inline
// scripts have run at parse time, and locale.js' DOMContentLoaded reformat
// is done.
export async function settled(window) {
	await new Promise((res) => {
		if (window.document.readyState === "complete") res();
		else window.addEventListener("load", res, { once: true });
	});
	return window;
}

// tick: let the pending promise chains settle (the dropdown's
// fetch → applyDict → applyLocale).
export const tick = () => new Promise((res) => setTimeout(res, 0));

// localTodayIso: "today" (the local wall clock, YYYY-MM-DD) as dater.js
// computes it.
export function localTodayIso() {
	const d = new Date();
	return d.getFullYear() + "-" +
		String(d.getMonth() + 1).padStart(2, "0") + "-" +
		String(d.getDate()).padStart(2, "0");
}

// click: a bubbling MouseEvent, as the browser delivers one.
export const click = (window, el) =>
	el.dispatchEvent(new window.MouseEvent("click", { bubbles: true }));
