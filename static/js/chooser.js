// chooser.js — dynamic choice rows for the vote form.
// The server renders the initial rows (one per stored choice on edit,
// each with a hidden field carrying the choice's stable ID; one empty
// row on create). This script handles add/remove, enforces the 20-row
// max, keeps the name attributes sequential, and updates the count.
(function () {
	const MAX = 20;
	const container = document.getElementById("choice-rows");
	const addBtn = document.getElementById("add-choice");
	const countEl = document.getElementById("choice-count");
	if (!container || !addBtn || !countEl) return;

	function rows() {
		return container.querySelectorAll(".choice-row");
	}

	function reindex() {
		rows().forEach(function (row, i) {
			const input = row.querySelector("input[type=text]");
			if (input) input.name = "choice" + i;
			const hid = row.querySelector('input[type="hidden"]');
			if (hid) hid.name = "choiceID" + i;
		});
	}

	function updateCount() {
		const n = rows().length;
		countEl.textContent = n + " / " + MAX;
		addBtn.disabled = n >= MAX;
	}

	function makeRow() {
		const row = document.createElement("div");
		row.className = "choice-row";

		const input = document.createElement("input");
		input.type = "text";
		input.maxLength = 100;
		input.required = true;

		const btn = document.createElement("button");
		btn.type = "button";
		btn.className = "choice-remove";
		btn.innerHTML = "&times;";
		const existing = container.querySelector(".choice-remove");
		if (existing) {
			btn.setAttribute("aria-label", existing.getAttribute("aria-label"));
			btn.setAttribute("title", existing.getAttribute("title"));
			btn.setAttribute("data-i18n-title", existing.getAttribute("data-i18n-title"));
		}
		btn.addEventListener("click", function () {
			if (rows().length <= 1) return;
			row.remove();
			reindex();
			updateCount();
		});

		row.appendChild(input);
		row.appendChild(btn);
		return row;
	}

	function addRow() {
		if (rows().length >= MAX) return null;
		const row = makeRow();
		container.appendChild(row);
		reindex();
		updateCount();
		return row.querySelector("input[type=text]");
	}

	addBtn.addEventListener("click", function () {
		addRow();
	});

	container.addEventListener("keydown", function (e) {
		if (e.key !== "Enter") return;
		const input = e.target;
		if (!input.matches(".choice-row input[type=text]")) return;
		e.preventDefault();
		const next = addRow();
		if (next) next.focus();
	});

	container.querySelectorAll(".choice-remove").forEach(function (btn) {
		btn.addEventListener("click", function () {
			if (rows().length <= 1) return;
			btn.closest(".choice-row").remove();
			reindex();
			updateCount();
		});
	});

	// The mode checkboxes and the voting type disable each other:
	//  - single-choice: yes/no and yes/maybe/no are invalid (a voter who
	//    may pick only one option can neither mark no nor hedge with a
	//    maybe), must-answer is invalid, and the default type is yes;
	//  - must-answer: yes is invalid (it would force a yes on every
	//    option), single-choice is invalid, and the default type is yes/no.
	// A selection that lands on a greyed radio moves to the mode's
	// default. The server re-checks every combination.
	var sc = document.getElementById("single-choice");
	var scNote = document.getElementById("single-choice-note");
	var ma = document.getElementById("must-answer");
	var maNote = document.getElementById("must-answer-note");
	var ytYes = document.getElementById("votetype-yes");
	var ytYesNo = document.getElementById("votetype-yes-no");
	var ytYesMaybeNo = document.getElementById("votetype-yes-maybe-no");
	var ytFreetext = document.getElementById("votetype-freetext");
	if (sc && ma && ytYes && ytYesNo && ytYesMaybeNo) {
		// Freetext is valid in every mode, so a freetext selection never
		// falls out — counting it here keeps the fallback from clobbering
		// it when a checkbox is toggled.
		var selected = function () {
			if (ytYes.checked) return ytYes;
			if (ytYesNo.checked) return ytYesNo;
			if (ytYesMaybeNo.checked) return ytYesMaybeNo;
			if (ytFreetext && ytFreetext.checked) return ytFreetext;
			return null;
		};
		var syncInterlock = function () {
			if (scNote) scNote.hidden = !sc.checked;
			if (maNote) maNote.hidden = !ma.checked;
			ma.disabled = sc.checked;
			sc.disabled = ma.checked;
			ytYesNo.disabled = sc.checked;
			ytYesMaybeNo.disabled = sc.checked;
			ytYes.disabled = ma.checked;
			var sel = selected();
			if (!sel || sel.disabled) {
				(sc.checked ? ytYes : ytYesNo).checked = true;
			}
		};
		sc.addEventListener("change", syncInterlock);
		ma.addEventListener("change", syncInterlock);
		syncInterlock();
	}

	updateCount();
})();
