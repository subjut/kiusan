// poll.js — unified answers table behaviour for the poll page (vanilla, no deps).
//
// The table holds a row per respondent and exactly one form row — the empty
// last row while the viewer has no answer, or a row opened for editing via
// ?edit=. Each form cell carries green / yellow / red toggle buttons
// (yellow only when the poll is not single-slot) and a hidden input
// holding the state ("g", "y" or "" = red X on submit; the red button
// simply selects the unset state). Single-slot polls grey out every other
// cell while one is marked; the server re-checks the constraint on submit.
// Trash icons fill and submit the page's hidden delete form.

(function () {
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

	function paintCell(cell) {
		var v = cell.getAttribute("data-status") || "";
		cell.classList.remove("st-g", "st-y", "st-r");
		cell.classList.add(v === "g" ? "st-g" : v === "y" ? "st-y" : "st-r");
		var g = cell.querySelector(".mark-g");
		if (g) g.setAttribute("aria-pressed", v === "g" ? "true" : "false");
		var y = cell.querySelector(".mark-y");
		if (y) y.setAttribute("aria-pressed", v === "y" ? "true" : "false");
		var r = cell.querySelector(".mark-r");
		if (r) r.setAttribute("aria-pressed", v === "" ? "true" : "false");
	}

	// Wire one answers form: its form cells and their hidden inputs.
	function wire(form, single) {
		var cells = Array.prototype.slice.call(form.querySelectorAll("td.form-cell"));

		function greyOut() {
			var marked = null;
			cells.forEach(function (c) {
				if (c.getAttribute("data-status")) marked = c;
			});
			cells.forEach(function (c) {
				var off = single && marked !== null && c !== marked;
				c.classList.toggle("greyed", off);
				c.querySelectorAll("button.mark").forEach(function (b) {
					b.disabled = off;
				});
			});
		}

		function setStatus(cell, v) {
			cell.setAttribute("data-status", v);
			var input = cell.querySelector('input[type="hidden"]');
			if (input) input.value = v;
			paintCell(cell);
			greyOut();
		}

		form.addEventListener("click", function (e) {
			var btn = e.target.closest("button.mark");
			if (!btn || btn.disabled) return;
			var cell = btn.closest("td.form-cell");
			if (!cell) return;
			var current = cell.getAttribute("data-status") || "";
			if (btn.classList.contains("mark-g")) {
				setStatus(cell, current === "g" ? "" : "g");
			} else if (btn.classList.contains("mark-y")) {
				setStatus(cell, current === "y" ? "" : "y");
			} else if (btn.classList.contains("mark-r")) {
				setStatus(cell, "");
			}
		});

		cells.forEach(function (c) {
			setStatus(c, c.getAttribute("data-status") || "");
		});
	}

	var main = document.getElementById("answer-form");
	if (main) {
		wire(main, main.getAttribute("data-single") === "true");
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
			// +1px: the collapsed table border paints ~0.5px beyond the
			// border box, so the raw width would leave a sub-pixel
			// overflow and raise a 1px scrollbar on every table.
			breakout.style.setProperty(
				"--tw",
				Math.ceil(table.getBoundingClientRect().width) + 2 + "px"
			);
		};
		setTableWidth();
		if (typeof ResizeObserver !== "undefined") {
			new ResizeObserver(setTableWidth).observe(table);
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
					? fill(t("poll.deleteWithName"), { name: name })
					: t("poll.deleteThis");
				if (!confirm(msg)) return;
				target.value = b.getAttribute("data-id") || "";
				delForm.submit();
			});
		});
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
				btn.textContent = t("poll.copied");
				setTimeout(function () { btn.textContent = t("poll.copy"); }, 1500);
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
})();
