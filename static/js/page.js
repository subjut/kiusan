// page.js — shared behaviour for the meeting and vote pages (vanilla,
// no deps). Loaded ahead of poll.js / vote.js, which use its t() / fill()
// globals.
//
// Wires everything the two page types have in common: the answers table's
// .table-breakout width, the answer-delete confirmations, the two-step
// poll/vote delete warning, the copyable link boxes and their copy
// buttons.

"use strict";

// UI strings from the server's catalogs (window.I18N, set by
// /i18n/boot and refreshed on language switches).
function t(key) {
	var d = window.I18N && window.I18N.t;
	return (d && d[key]) || key;
}
// One-pass {placeholder} substitution; the catalogs never use {{...}}
// and the single replace pass keeps values from re-entering it.
function fill(s, vars) {
	return s.replace(/\{(\w+)\}/g, function (m, k) {
		return Object.prototype.hasOwnProperty.call(vars, k) ? vars[k] : m;
	});
}

// The answers table's .table-breakout wrapper is sized to the table's
// rendered width via --tw: pure-CSS intrinsic sizing did not
// reliably resolve the table's width through the overflow:auto scroll
// box, so it is measured here. Re-measure when the table resizes
// (font load, zoom).
var breakout = document.querySelector(".table-breakout");
var table = document.querySelector(".answers-table");
if (breakout && table) {
	var setTableWidth = function () {
		// +2px: the collapsed table border paints ~0.5px beyond the
		// border box, so the raw width would leave a sub-pixel
		// overflow and raise a 1px scrollbar on every table.
		breakout.style.setProperty(
			"--tw",
			Math.ceil(table.getBoundingClientRect().width) + 2 + "px"
		);
	};
	// The sticky thead's rows pin at the cumulative height of the rows
	// above them (--th-2 / --th-3 on the table, read by the CSS); the
	// date label wraps to 1–3 lines, so the offsets are measured, not
	// written. The .table-sticky class activates the pinning only once
	// measured — without JS the table keeps the plain page-scrolled
	// layout.
	var thead = table.querySelector("thead");
	var setHeaderOffsets = function () {
		if (!thead) return;
		var y = 0;
		for (var i = 0; i < thead.rows.length; i++) {
			table.style.setProperty("--th-" + (i + 1), Math.ceil(y) + "px");
			y += thead.rows[i].offsetHeight;
		}
		table.classList.add("table-sticky");
	};
	var measure = function () {
		setTableWidth();
		setHeaderOffsets();
	};
	measure();
	if (typeof ResizeObserver !== "undefined") {
		new ResizeObserver(measure).observe(table);
	}
	// A locale switch rewrites the header's date labels and can change a
	// row's height — re-measure the offsets then, too.
	if (window.Locale) {
		window.Locale.onChange(setHeaderOffsets);
	}
}

// Deleting an answer is destructive: confirm on click, then fill and
// submit the page's hidden delete form.
var delForm = document.getElementById("del-form");
if (delForm) {
	var target = delForm.querySelector('input[name="respondentid"]');
	Array.prototype.forEach.call(document.querySelectorAll(".del-btn"), function (b) {
		b.addEventListener("click", function () {
			var name = b.getAttribute("data-name") || "";
			var msg = name
				? fill(t("page.deleteWithName"), { name: name })
				: t("page.deleteThis");
			if (!confirm(msg)) return;
			target.value = b.getAttribute("data-id") || "";
			delForm.submit();
		});
	});
}

// Two-step confirmation: the trigger button opens the inline warning
// (a role=alert, so screen readers announce it) and its confirm button
// submits the form. The confirm button is the only submit path, so a
// plain browser (no JS) never reaches it — the action stays an explicit
// two-step confirmation. Delete-poll uses it (the site's most
// destructive action), and re-open does while final marks are set
// (re-opening deletes them).
function wireTwoStep(form, trigger, warning, cancel) {
	var confirmBtn = warning.querySelector('button[type="submit"]');
	trigger.addEventListener("click", function () {
		warning.hidden = false;
		confirmBtn.focus();
	});
	cancel.addEventListener("click", function () {
		warning.hidden = true;
		trigger.focus();
	});
}
var pollDel = document.getElementById("poll-del-form");
var delWarn = document.querySelector(".delete-warning");
if (pollDel && delWarn) {
	wireTwoStep(
		pollDel,
		pollDel.querySelector(".delete-poll-btn"),
		delWarn,
		delWarn.querySelector(".delete-cancel")
	);
}
var pollReopen = document.getElementById("poll-reopen-form");
var reopenWarn = document.querySelector(".reopen-warning");
if (pollReopen && reopenWarn) {
	wireTwoStep(
		pollReopen,
		pollReopen.querySelector(".reopen-poll-btn"),
		reopenWarn,
		reopenWarn.querySelector(".reopen-cancel")
	);
}

// Clicking a link box selects the whole URL for copying.
Array.prototype.forEach.call(document.querySelectorAll("input.copybox"), function (i) {
	i.addEventListener("click", function () { i.select(); });
});

// Copy buttons in front of the link boxes: copy the URL, flash
// "Copied!". navigator.clipboard needs a secure context (https /
// localhost); on plain http fall back to select + execCommand on the
// readonly box.
function legacyCopy(box, ok) {
	box.select();
	var done = false;
	try { done = document.execCommand("copy"); } catch (e) { /* denied */ }
	if (done) ok();
}
Array.prototype.forEach.call(document.querySelectorAll(".copy-btn"), function (btn) {
	btn.addEventListener("click", function () {
		var field = btn.closest(".link-field");
		var box = field ? field.querySelector("input.copybox") : null;
		if (!box) return;
		var done = function () {
			btn.textContent = t("page.copied");
			setTimeout(function () { btn.textContent = t("page.copy"); }, 1500);
		};
		if (navigator.clipboard && navigator.clipboard.writeText) {
			navigator.clipboard.writeText(box.value).then(done, function () {
				legacyCopy(box, done);
			});
		} else {
			legacyCopy(box, done);
		}
	});
});

// Size each link box to its URL: the boxes are monospace, so width is
// chars x char-width + padding/borders (border-box). The CSS
// max-width: 100% caps it at the container for very long domains, and
// the value then scrolls in place. The links are static per page load,
// so measure once.
(function () {
	var boxes = document.querySelectorAll("input.copybox");
	if (!boxes.length) return;
	var cs = getComputedStyle(boxes[0]);
	var probe = document.createElement("span");
	probe.textContent = "0000000000";
	probe.style.position = "absolute";
	probe.style.visibility = "hidden";
	probe.style.whiteSpace = "pre";
	probe.style.fontFamily = cs.fontFamily;
	probe.style.fontSize = cs.fontSize;
	document.body.appendChild(probe);
	var charW = probe.getBoundingClientRect().width / probe.textContent.length;
	probe.remove();
	var extra = parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight) +
		parseFloat(cs.borderLeftWidth) + parseFloat(cs.borderRightWidth);
	boxes.forEach(function (i) {
		i.style.width = Math.ceil(i.value.length * charW) + extra + "px";
	});
})();
