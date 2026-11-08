// poll.js — unified answers table behaviour for the poll page (vanilla, no deps).
//
// The table holds a row per respondent and exactly one form row — the empty
// last row while the viewer has no answer, or a row opened for editing via
// ?edit=. Each form cell carries green / yellow toggle buttons (yellow only
// when the poll is not single-slot) and a hidden input holding the state
// ("g", "y" or "" — the empty state renders red with an ✗ in the
// submitted-answers rows; there is no "no" button, toggling a mark off
// simply clears it). Single-slot polls grey out every other cell while one
// is marked; the server re-checks the constraint on submit.
// Trash icons fill and submit the page's hidden delete form.

(function () {
	"use strict";

	function paintCell(cell) {
		var v = cell.getAttribute("data-status") || "";
		cell.classList.remove("st-g", "st-y", "st-r");
		if (v === "g") cell.classList.add("st-g");
		else if (v === "y") cell.classList.add("st-y");
		// Unmarked form cells stay neutral (no st-r) — the red tint with an
		// ✗ appears only in the submitted-answers rows.
		var g = cell.querySelector(".mark-g");
		if (g) g.setAttribute("aria-pressed", v === "g" ? "true" : "false");
		var y = cell.querySelector(".mark-y");
		if (y) y.setAttribute("aria-pressed", v === "y" ? "true" : "false");
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

})();
