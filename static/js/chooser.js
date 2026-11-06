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

	// The single-choice note is visible only while its checkbox is on.
	var sc = document.getElementById("single-choice");
	var scNote = document.getElementById("single-choice-note");
	if (sc && scNote) {
		var syncNote = function () { scNote.hidden = !sc.checked; };
		sc.addEventListener("change", syncNote);
		syncNote();
	}

	updateCount();
})();
