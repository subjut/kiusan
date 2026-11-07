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
// every option carries a selection. The server re-checks every constraint
// on submit. Trash icons fill and submit the page's hidden delete form.
// constraint on submit. Trash icons fill and submit the page's hidden
// delete form.

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
			};
			main.addEventListener("click", refreshSubmit);
			main.addEventListener("input", refreshSubmit);
			refreshSubmit();
		}
	}

	// The answers table's .table-breakout wrapper is sized to the table's
	// rendered width via --tw (see poll.js). Re-measure when the table
	// resizes (font load, zoom).
	var breakout = document.querySelector(".table-breakout");
	var table = document.querySelector(".answers-table");
	if (breakout && table) {
		var setTableWidth = function () {
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

	// Deleting the vote is the site's most destructive action: the button
	// opens the inline warning (a role=alert) and its confirm button
	// submits the form. The confirm button is the only submit path, so a
	// plain browser (no JS) never reaches it.
	var pollDel = document.getElementById("poll-del-form");
	var warning = document.querySelector(".delete-warning");
	if (pollDel && warning) {
		var delBtn = pollDel.querySelector(".delete-poll-btn");
		var confirmBtn = warning.querySelector('button[type="submit"]');
		var cancelBtn = warning.querySelector(".delete-cancel");
		delBtn.addEventListener("click", function () {
			warning.hidden = false;
			confirmBtn.focus();
		});
		cancelBtn.addEventListener("click", function () {
			warning.hidden = true;
			delBtn.focus();
		});
	}

	// Clicking a link box selects the whole URL for copying.
	Array.prototype.forEach.call(document.querySelectorAll("input.copybox"), function (i) {
		i.addEventListener("click", function () { i.select(); });
	});

	// Copy buttons in front of the link boxes: copy the URL, flash
	// "Copied!". navigator.clipboard needs a secure context; on plain http
	// fall back to select + execCommand on the readonly box.
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

	// Size each link box to its URL (see poll.js).
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
