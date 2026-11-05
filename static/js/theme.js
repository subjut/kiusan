// theme.js — the header's light/dark toggle (vanilla, no deps).
//
// Pages load in the viewer's theme with no flash: the server renders the
// explicit choice as a class on <html> from the kiusan-theme cookie
// ("dark" / "light"), and without a cookie the CSS media query follows
// the system setting. This script (loaded in the head, like /i18n/boot)
// only keeps the page in sync after load: while no cookie is set it
// follows live system-setting changes, and the toggle button flips the
// theme, mirrors it onto the same classes, and persists it to the
// cookie (1 year, like kiusan-locale).
//
// window.I18N is set by /i18n/boot (loaded before this file); the
// toggle's localized aria-label — which always offers the switch to the
// *other* theme — is refreshed from it on every flip and on every
// language change (locale.js updates window.I18N before firing its
// change listeners).

(function () {
	"use strict";

	var COOKIE = "kiusan-theme";
	var MAX_AGE = 31536000; // 1 year: a display preference, not an identity

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

	function cookieSet(v) {
		var age = v ? "; max-age=" + MAX_AGE : "; max-age=0";
		document.cookie = COOKIE + "=" + v + "; path=/" + age +
			"; samesite=Lax";
	}

	var darkQuery = "(prefers-color-scheme: dark)";
	function systemDark() {
		return window.matchMedia &&
			window.matchMedia(darkQuery).matches;
	}

	// The effective theme: an explicit (validated) cookie choice wins,
	// else the system setting.
	function effective() {
		var c = cookieValue();
		if (c === "dark" || c === "light") return c;
		return systemDark() ? "dark" : "light";
	}

	var root = document.documentElement;
	var btn = null; // wired on DOMContentLoaded (the script sits in the head)

	function applyLabel() {
		// The button always offers the switch to the *other* theme.
		var t = window.I18N && window.I18N.t;
		if (!btn || !t) return;
		var key = effective() === "dark" ? "nav.themeLight" : "nav.themeDark";
		var v = t[key];
		if (v != null) btn.setAttribute("aria-label", v);
	}

	function apply(t) {
		root.classList.toggle("theme-dark", t === "dark");
		root.classList.toggle("theme-light", t === "light");
		applyLabel();
	}

	function wireSystemChanges() {
		var mq = window.matchMedia && window.matchMedia(darkQuery);
		if (!mq) return;
		var onSystem = function (e) {
			// Follow the system only while no explicit choice is stored.
			if (cookieValue() !== "") return;
			apply(e.matches ? "dark" : "light");
		};
		if (mq.addEventListener) mq.addEventListener("change", onSystem);
		else if (mq.addListener) mq.addListener(onSystem);
	}

	wireSystemChanges();

	document.addEventListener("DOMContentLoaded", function () {
		btn = document.getElementById("theme-toggle");
		// Mirror the effective theme onto the classes (and fix the
		// server's aria-label, which cannot see the system setting).
		apply(effective());
		if (btn) {
			btn.addEventListener("click", function () {
				var next = effective() === "dark" ? "light" : "dark";
				cookieSet(next);
				apply(next);
			});
			// Re-label the button after a language switch.
			if (window.Locale && window.Locale.onChange) {
				window.Locale.onChange(applyLabel);
			}
		}
	});
})();
