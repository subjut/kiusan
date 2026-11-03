// locale.js — browser-locale date display for the poll + form pages
// (vanilla, no deps).
//
// Every date/time *value* the server rendered (its English labels stay as
// the no-JS fallback) is rewritten in place with Intl.DateTimeFormat in the
// viewer's locale: the `kiusan-locale` cookie set by the "Display"
// dropdown wins, else navigator.language. Date values are always shown in
// the poll's timezone — the [data-tz] container attribute — so all viewers
// see the poll's wall time, just spelled in their own language; the
// viewer's own zone is never used for the values.
//
// dater.js (the form's calendar) formats its wall-clock picker labels
// through Locale.dtf (no timeZone) and re-renders on locale changes.

(function () {
	"use strict";

	var COOKIE = "kiusan-locale";
	var MAX_AGE = 31536000; // 1 year: a display preference, not an identity
	// Curated dropdown list; the browser's own language is auto-inserted
	// at its alphabetical position when it is not already listed (same
	// pattern as the timezone prefill in dater.js).
	var LOCALES = ["en", "de", "de-CH", "fr", "es", "it", "pt", "pt-BR",
		"nl", "pl", "sv", "tr", "ja", "ko", "zh"];
	// The options show native language names; the value stays the BCP-47
	// tag. A browser language not in the list shows its raw tag.
	var LOCALE_NAMES = {
		"en": "English", "de": "Deutsch", "de-CH": "Deutsch (Schweiz)",
		"fr": "Français", "es": "Español", "it": "Italiano",
		"pt": "Português", "pt-BR": "Português (Brasil)",
		"nl": "Nederlands", "pl": "Polski", "sv": "Svenska",
		"tr": "Türkçe", "ja": "日本語", "ko": "한국어", "zh": "中文"
	};

	function localeName(loc) {
		return LOCALE_NAMES[loc] || loc;
	}

	var formatters = {}; // "locale|opts-json" -> Intl.DateTimeFormat
	var listeners = [];

	function valid(loc) {
		if (!loc) return false;
		try {
			new Intl.DateTimeFormat(loc);
			return true;
		} catch (e) {
			return false;
		}
	}

	function cookieValue() {
		var parts = document.cookie.split(";");
		for (var i = 0; i < parts.length; i++) {
			var kv = parts[i].split("=");
			if (kv[0].replace(/^ /, "") === COOKIE) {
				return kv.slice(1).join("=");
			}
		}
		return "";
	}

	function cookieSet(loc) {
		var age = loc ? "; max-age=" + MAX_AGE : "; max-age=0";
		document.cookie = COOKIE + "=" + loc + "; path=/" + age +
			"; samesite=lax";
	}

	function browserLocale() {
		return navigator.language || "en";
	}

	// The viewer's locale: an explicit (validated) cookie choice wins,
	// else the browser default.
	function resolve() {
		var c = cookieValue();
		return valid(c) ? c : browserLocale();
	}

	var locale = resolve();

	function dtf(loc, opts) {
		var key = loc + "|" + JSON.stringify(opts);
		if (!formatters[key]) {
			formatters[key] = new Intl.DateTimeFormat(loc, opts);
		}
		return formatters[key];
	}

	// The poll's zone travels in data-tz (IANA name, "UTC" when the poll
	// has none). Passed as the timeZone option, the calendar parts come out
	// in the poll's wall time on every viewer's machine.
	function withTz(opts, tz) {
		var o = {};
		for (var k in opts) o[k] = opts[k];
		if (tz) o.timeZone = tz;
		return o;
	}

	function offset(tz) {
		try {
			var parts = dtf("en-US",
				withTz({ timeZoneName: "longOffset" }, tz))
				.formatToParts(new Date());
			for (var i = 0; i < parts.length; i++) {
				if (parts[i].type === "timeZoneName") return parts[i].value;
			}
		} catch (e) { /* unknown zone: no offset */ }
		return "";
	}

	// Rewrite every server-rendered date value in the viewer's locale.
	function reformat() {
		var root = document.querySelector("[data-tz]");
		if (!root) return; // the form page has no poll zone; dater.js owns its labels
		var tz = root.getAttribute("data-tz") || "UTC";
		var loc = locale;
		var i, els;

		function fmt(el, opts) {
			var d = new Date(el.getAttribute("data-slot"));
			if (isNaN(d)) return;
			try {
				el.textContent = dtf(loc, withTz(opts, tz)).format(d);
			} catch (e) {
				el.textContent = dtf(loc, opts).format(d);
			}
		}

		// Year row.
		els = root.querySelectorAll(".th-year[data-slot]");
		for (i = 0; i < els.length; i++) fmt(els[i], { year: "numeric" });

		// Date row: day/month/weekday in the server-chosen line count.
		els = root.querySelectorAll(".th-date[data-slot]");
		for (i = 0; i < els.length; i++) {
			var d = new Date(els[i].getAttribute("data-slot"));
			if (isNaN(d)) continue;
			var lines = parseInt(els[i].getAttribute("data-lines"), 10) || 1;
			try {
				var day = dtf(loc, withTz({ day: "numeric" }, tz)).format(d);
				var mon = dtf(loc, withTz({ month: "short" }, tz)).format(d);
				var wk = dtf(loc, withTz({ weekday: "short" }, tz)).format(d);
				if (lines >= 3) {
					els[i].textContent = day + "\n" + mon + "\n" + wk;
				} else if (lines === 2) {
					els[i].textContent = day + " " + mon + "\n" + wk;
				} else {
					els[i].textContent = day + " " + mon + " " + wk;
				}
				els[i].title = dtf(loc, withTz(
					{ weekday: "long", day: "numeric", month: "long",
						year: "numeric" }, tz)).format(d);
			} catch (e) { /* zone the browser lacks: keep the server text */ }
		}

		// Time sub-row (midnight slots carry no slot and keep the dash).
		els = root.querySelectorAll(".th-time[data-slot]");
		for (i = 0; i < els.length; i++) {
			fmt(els[i], { hour: "numeric", minute: "2-digit" });
		}

		// "Poll ends" / "ended on" dates.
		els = root.querySelectorAll(".f-date[data-slot]");
		for (i = 0; i < els.length; i++) {
			fmt(els[i], els[i].getAttribute("data-style") === "long"
				? { day: "numeric", month: "long", year: "numeric" }
				: { day: "numeric", month: "short", year: "numeric" });
		}

		// Timezone line: append the poll's UTC offset.
		var tzName = root.querySelector(".tz-name");
		if (tzName) {
			var base = tzName.getAttribute("data-base") ||
				tzName.textContent;
			var off = offset(tz);
			tzName.textContent = off ? base + " (" + off + ")" : base;
		}
	}

	// ---- "Display" dropdown ----------------------------------------------
	var sel = document.getElementById("locale");
	if (sel) {
		var browser = browserLocale();
		var list = LOCALES.slice();
		if (list.indexOf(browser) < 0) {
			var i = 0;
			while (i < list.length && list[i] < browser) i++;
			list.splice(i, 0, browser);
		}
		var c0 = cookieValue();
		var chosen = valid(c0) ? c0 : "";
		var html = '<option value="">Browser default (' + localeName(browser) + ")</option>";
		for (i = 0; i < list.length; i++) {
			html += '<option value="' + list[i] + '"' +
				(list[i] === chosen ? " selected" : "") + ">" +
				localeName(list[i]) + "</option>";
		}
		sel.innerHTML = html;
		sel.value = chosen;
		sel.addEventListener("change", function () {
			cookieSet(sel.value);
			locale = resolve();
			reformat();
			for (var j = 0; j < listeners.length; j++) listeners[j]();
		});
	}

	// The script sits in the site header, which parses before the page
	// body: defer the first reformat until the document is complete.
	if (document.readyState === "loading") {
		document.addEventListener("DOMContentLoaded", reformat);
	} else {
		reformat();
	}

	// For dater.js: the current locale, a cached formatter factory (no
	// timeZone — the picker is wall-clock), and change notification.
	window.Locale = {
		get: function () { return locale; },
		dtf: function (opts) { return dtf(locale, opts); },
		onChange: function (fn) { listeners.push(fn); }
	};
})();
