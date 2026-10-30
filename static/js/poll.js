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
				if (!confirm(name ? "Delete " + name + "'s answer?" : "Delete this answer?")) return;
				target.value = b.getAttribute("data-id") || "";
				delForm.submit();
			});
		});
	}

	// Clicking a link box selects the whole URL for copying.
	Array.prototype.forEach.call(document.querySelectorAll("input.copybox"), function (i) {
		i.addEventListener("click", function () { i.select(); });
	});
})();
