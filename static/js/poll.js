// poll.js — answer-table behaviour for the poll page (vanilla, no deps).
//
// Model: one table row per slot, each with green / yellow toggle buttons
// (yellow only when the poll is not single-slot) and a hidden input
// carrying the state ("g", "y" or "" = red X on submit). Single-slot
// polls grey out every other row while one is marked; the server
// re-checks the constraint on submit, the grey-out is a convenience.

(function () {
	"use strict";

	function paintRow(row) {
		var v = row.getAttribute("data-status") || "";
		row.classList.toggle("sel-g", v === "g");
		row.classList.toggle("sel-y", v === "y");
		var st = row.querySelector(".status");
		if (st) {
			st.textContent = v === "g" ? "\u2713" : v === "y" ? "(\u2713)" : "\u2717";
			st.className = "status " + (v === "g" ? "st-g" : v === "y" ? "st-y" : "st-r");
		}
		var gb = row.querySelector(".mark-g");
		if (gb) gb.setAttribute("aria-pressed", v === "g" ? "true" : "false");
		var yb = row.querySelector(".mark-y");
		if (yb) yb.setAttribute("aria-pressed", v === "y" ? "true" : "false");
	}

	// Wire one answer table: the form that owns the rows and their hidden
	// inputs (the own-answer form or one of the admin's per-respondent
	// cards).
	function wire(root, single) {
		var rows = Array.prototype.slice.call(root.querySelectorAll("tr[data-slot]"));

		function greyOut() {
			var marked = null;
			rows.forEach(function (r) {
				if (r.getAttribute("data-status")) marked = r;
			});
			rows.forEach(function (r) {
				var off = single && marked !== null && r !== marked;
				r.classList.toggle("greyed", off);
				r.querySelectorAll("button.mark").forEach(function (b) {
					b.disabled = off;
				});
			});
		}

		function setStatus(slot, v) {
			var row = null;
			rows.forEach(function (r) {
				if (r.getAttribute("data-slot") === slot) row = r;
			});
			if (!row) return;
			row.setAttribute("data-status", v);
			var input = root.querySelector('input[name="a' + slot + '"]');
			if (input) input.value = v;
			paintRow(row);
			greyOut();
		}

		root.addEventListener("click", function (e) {
			var btn = e.target.closest("button.mark");
			if (!btn || btn.disabled) return;
			var row = btn.closest("tr[data-slot]");
			if (!row) return;
			var slot = row.getAttribute("data-slot");
			var current = row.getAttribute("data-status") || "";
			if (btn.classList.contains("mark-g")) {
				setStatus(slot, current === "g" ? "" : "g");
			} else if (btn.classList.contains("mark-y")) {
				setStatus(slot, current === "y" ? "" : "y");
			}
		});

		rows.forEach(function (r) {
			setStatus(r.getAttribute("data-slot"), r.getAttribute("data-status") || "");
		});
	}

	var main = document.getElementById("answer-form");
	if (main) {
		wire(main, main.getAttribute("data-single") === "true");
	}

	Array.prototype.forEach.call(document.querySelectorAll("form.resp-form"), function (f) {
		wire(f, f.getAttribute("data-single") === "true");
	});

	// Deleting an answer is destructive: confirm on submit.
	Array.prototype.forEach.call(
		document.querySelectorAll("form.answer-delete, form.resp-del"),
		function (f) {
			f.addEventListener("submit", function (e) {
				var msg = f.classList.contains("resp-del")
					? "Delete this respondent's answer?"
					: "Delete your answer for this poll?";
				if (!confirm(msg)) e.preventDefault();
			});
		}
	);

	// Clicking a link box selects the whole URL for copying.
	Array.prototype.forEach.call(document.querySelectorAll("input.copybox"), function (i) {
		i.addEventListener("click", function () { i.select(); });
	});
})();
