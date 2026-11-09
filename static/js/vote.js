// vote.js — unified answers table behaviour for the vote page (vanilla,
// no deps).
//
// Mirror of poll.js for the choice table: the table holds a row per voter
// and exactly one form row — the empty last row while the viewer has no
// answer, or a row opened for editing via ?edit=. The form cells follow
// the vote's type (data-type on the form): yes cells carry one yes-toggle,
// yes-no cells yes / no toggles, yes-maybe-no cells yes / maybe / no
// toggles, free-text cells one text input per option. An untouched cell
// stays neutral in every type. In a single-choice vote (data-single) a
// yes set on one option clears the previously chosen one — at any moment
// there is only one yes — and only one free-text input may be filled. In
// a must-answer vote (data-must) the submit button stays disabled until
// every option carries a selection, and the instruction below the form
// reads amber, green once the answer is complete (amber again when an
// option is deselected), and red with a short shake after a click on the
// greyed button (scrolled into view first). The server re-checks every
// constraint on submit. Trash icons fill and submit the page's hidden delete form.

(function () {
	"use strict";

	function paintCell(cell) {
		var v = cell.getAttribute("data-status") || "";
		cell.classList.remove("st-g", "st-y", "st-r");
		if (v === "y") cell.classList.add("st-g");
		else if (v === "m") cell.classList.add("st-y");
		else if (v === "n") cell.classList.add("st-r");
		var g = cell.querySelector("button.mark-g");
		if (g) g.setAttribute("aria-pressed", v === "y" ? "true" : "false");
		var m = cell.querySelector("button.mark-y");
		if (m) m.setAttribute("aria-pressed", v === "m" ? "true" : "false");
		var r = cell.querySelector("button.mark-r");
		if (r) r.setAttribute("aria-pressed", v === "n" ? "true" : "false");
	}

	// Wire one mark-mode answers form: its form cells and their hidden
	// inputs.
	function wireMarkForm(form, single) {
		var cells = Array.prototype.slice.call(form.querySelectorAll("td.form-cell"));

		function setStatus(cell, v) {
			cell.setAttribute("data-status", v);
			var input = cell.querySelector('input[type="hidden"]');
			if (input) input.value = v;
			paintCell(cell);
		}

		form.addEventListener("click", function (e) {
			var btn = e.target.closest("button.mark");
			if (!btn) return;
			var cell = btn.closest("td.form-cell");
			if (!cell) return;
			var current = cell.getAttribute("data-status") || "";
			var v;
			if (btn.classList.contains("mark-g")) {
				v = current === "y" ? "" : "y";
				if (single && v === "y") {
					// Switching the yes over: the previously chosen
					// option goes back to neutral, so the click lands a
					// new selection — exactly one yes stays set.
					cells.forEach(function (c) {
						if (c !== cell && c.getAttribute("data-status") === "y") {
							setStatus(c, "");
						}
					});
				}
			} else if (btn.classList.contains("mark-y")) {
				v = current === "m" ? "" : "m";
			} else {
				v = current === "n" ? "" : "n";
			}
			setStatus(cell, v);
		});

		cells.forEach(function (c) {
			setStatus(c, c.getAttribute("data-status") || "");
		});
	}

	// Wire one free-text answers form: single-choice votes allow exactly
	// one filled note — filling one clears and disables the rest,
	// emptying it re-enables them.
	function wireFreeForm(form, single) {
		if (!single) return;
		var inputs = Array.prototype.slice.call(
			form.querySelectorAll("td.form-cell input[type=text]")
		);
		inputs.forEach(function (inp) {
			inp.addEventListener("input", function () {
				var has = inp.value.trim() !== "";
				inputs.forEach(function (o) {
					if (o === inp) return;
					if (has) {
						o.value = "";
						o.disabled = true;
					} else {
						o.disabled = false;
					}
				});
			});
		});
	}

	var main = document.getElementById("answer-form");
	if (main) {
		var single = main.getAttribute("data-single") === "true";
		var type = main.getAttribute("data-type") || "";
		if (type === "freetext") {
			wireFreeForm(main, single);
		} else {
			wireMarkForm(main, single);
		}

		// A must-answer vote keeps the submit button disabled until every
		// option carries a selection.
		if (main.getAttribute("data-must") === "true") {
			var submitBtn = main.querySelector('button[type="submit"]');
			var note = main.querySelector("p.must-hint");
			var complete = function () {
				if (type === "freetext") {
					return Array.prototype.every.call(
						main.querySelectorAll("td.form-cell input[type=text]"),
						function (i) { return i.value.trim() !== ""; }
					);
				}
				return Array.prototype.every.call(
					main.querySelectorAll("td.form-cell"),
					function (c) { return (c.getAttribute("data-status") || "") !== ""; }
				);
			};
			var refreshSubmit = function () {
				if (submitBtn) submitBtn.disabled = !complete();
				if (note) {
					// Green while the answer is complete, amber again as soon
					// as an option is deselected; the click flash (.err) is
					// dropped once the answer is complete.
					if (complete()) {
						note.classList.remove("err");
						note.classList.add("ok");
					} else {
						note.classList.remove("ok");
					}
				}
			};

			// A disabled button swallows its own click events, so a click on
			// it lands on an ancestor — hit-test the coordinates against the
			// button's rect to catch it.
			var flashTimer = null;
			var flashNote = function () {
				if (!note) return;
				if (flashTimer) {
					window.clearTimeout(flashTimer);
					flashTimer = null;
				}
				var shake = function () {
					// Remove before re-adding (reflow between) so the shake
					// restarts on a repeated click.
					note.classList.remove("err");
					void note.offsetWidth;
					note.classList.add("err");
				};
				var r = note.getBoundingClientRect();
				if (r.top >= 0 && r.bottom <= window.innerHeight) {
					shake();
				} else {
					note.scrollIntoView({ block: "center", behavior: "smooth" });
					flashTimer = window.setTimeout(function () {
						flashTimer = null;
						shake();
					}, 250);
				}
			};
			if (note) {
				note.addEventListener("animationend", function () {
					note.classList.remove("err");
				});
			}
			main.addEventListener("click", function (e) {
				refreshSubmit();
				if (!submitBtn || !note || !submitBtn.disabled) return;
				var b = submitBtn.getBoundingClientRect();
				if (
					e.clientX >= b.left &&
					e.clientX <= b.right &&
					e.clientY >= b.top &&
					e.clientY <= b.bottom
				) {
					flashNote();
				}
			});
			main.addEventListener("input", refreshSubmit);
			refreshSubmit();
		}
	}

})();
