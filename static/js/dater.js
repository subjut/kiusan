// dater.js — date/time picker for the meeting form (vanilla, no deps).
//
// Model: candidate slots = selected dates × selected times. Dates are
// picked in a month calendar (multi-select), times are added as chips;
// removing a date or a time removes all of its slots. All state is plain
// YYYY-MM-DD / HH:MM strings, so there are no Date-object timezone
// pitfalls — "today" comes from the user's browser on purpose, since the
// earliest selectable date should follow the user, not the server.

(function () {
	"use strict";

	var form = document.getElementById("form");
	if (!form) return;

	// ---- state -----------------------------------------------------------
	var dates = {}; // "YYYY-MM-DD" -> true
	var times = {}; // "HH:MM" -> true

	// When the form edits an existing poll, the server seeds the picker
	// state via data-dates / data-times (comma-separated lists).
	(function seed() {
		(function (list, into) {
			list.split(",").forEach(function (v) {
				if (v) into[v] = true;
			});
		})(form.getAttribute("data-dates") || "", dates);
		(function (list, into) {
			list.split(",").forEach(function (v) {
				if (v) into[v] = true;
			});
		})(form.getAttribute("data-times") || "", times);
	})();

	var dateChips = document.getElementById("date-chips");
	var timeChips = document.getElementById("time-chips");
	var timeInput = document.getElementById("time-input");
	var preview = document.getElementById("slot-preview");
	var grid = document.getElementById("cal-grid");
	var calTitle = document.getElementById("cal-title");

	var DAY_NAMES = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
	var MONTH_NAMES = ["January", "February", "March", "April", "May",
		"June", "July", "August", "September", "October", "November",
		"December"];

	function pad(n, w) {
		var s = String(n);
		while (s.length < w) s = "0" + s;
		return s;
	}

	// Local "today" as YYYY-MM-DD (client side on purpose: min-date should
	// follow the user, not the server).
	function localToday() {
		var d = new Date();
		return d.getFullYear() + "-" + pad(d.getMonth() + 1, 2) + "-" +
			pad(d.getDate(), 2);
	}

	function iso(y, m, d) {
		return y + "-" + pad(m + 1, 2) + "-" + pad(d, 2);
	}

	function daysInMonth(y, m) {
		return new Date(y, m + 1, 0).getDate();
	}

	// ---- calendar ----------------------------------------------------------
	// The view starts on the first seeded date (edit mode) or the user's
	// current month.
	var view = (function () {
		var base = localToday();
		for (var d in dates) { base = d; break; }
		var t = base.split("-");
		return { y: +t[0], m: +t[1] - 1 }; // m: 0-based
	})();

	function renderCalendar() {
		var min = localToday();
		calTitle.textContent = MONTH_NAMES[view.m] + " " + view.y;

		var html = "<tr>";
		for (var n = 0; n < DAY_NAMES.length; n++) {
			html += '<th scope="col">' + DAY_NAMES[n] + "</th>";
		}
		html += "</tr>";

		var first = new Date(view.y, view.m, 1).getDay(); // 0 = Sunday
		var dim = daysInMonth(view.y, view.m);
		// A month always fits in six week-rows; cells outside the month
		// are left empty.
		for (var w = 0; w < 6; w++) {
			html += "<tr>";
			for (var col = 0; col < 7; col++) {
				var idx = w * 7 + col - first + 1;
				if (idx < 1 || idx > dim) {
					html += "<td></td>";
					continue;
				}
				var dateStr = iso(view.y, view.m, idx);
				var past = dateStr < min;
				var cls = "day";
				if (dates[dateStr]) cls += " selected";
				html += '<td><button type="button" class="' + cls + '"' +
					(past ? " disabled" : "") + ' data-date="' + dateStr +
					'" title="' + dateStr + '">' + idx + "</button></td>";
			}
			html += "</tr>";
		}
		grid.innerHTML = html;

		// Don't allow navigating to months entirely before today.
		document.getElementById("cal-prev").disabled =
			iso(view.y, view.m, 1).slice(0, 7) <= localToday().slice(0, 7);
	}

	document.getElementById("cal-prev").addEventListener("click", function () {
		view.m--;
		if (view.m < 0) { view.m = 11; view.y--; }
		renderCalendar();
	});
	document.getElementById("cal-next").addEventListener("click", function () {
		view.m++;
		if (view.m > 11) { view.m = 0; view.y++; }
		renderCalendar();
	});
	grid.addEventListener("click", function (e) {
		var btn = e.target.closest("button.day");
		if (!btn || btn.disabled) return;
		var d = btn.getAttribute("data-date");
		if (dates[d]) {
			delete dates[d];
		} else {
			dates[d] = true;
		}
		renderAll();
	});

	// ---- chips ---------------------------------------------------------------
	function sorted(obj) {
		return Object.keys(obj).sort();
	}

	function renderDateChips() {
		dateChips.innerHTML = sorted(dates).map(function (d) {
			return '<span class="chip">' + d +
				'<button type="button" class="chip-x" data-date="' + d +
				'" title="Remove ' + d + '">✕</button></span>';
		}).join("");
	}

	function renderTimeChips() {
		timeChips.innerHTML = sorted(times).map(function (t) {
			return '<span class="chip">' + t +
				'<button type="button" class="chip-x" data-time="' + t +
				'" title="Remove ' + t + '">✕</button></span>';
		}).join("");
	}

	dateChips.addEventListener("click", function (e) {
		var x = e.target.closest(".chip-x");
		if (!x) return;
		delete dates[x.getAttribute("data-date")];
		renderAll();
	});
	timeChips.addEventListener("click", function (e) {
		var x = e.target.closest(".chip-x");
		if (!x) return;
		delete times[x.getAttribute("data-time")];
		renderAll();
	});
	document.getElementById("clear-dates").addEventListener("click", function () {
		dates = {};
		renderAll();
	});
	document.getElementById("clear-times").addEventListener("click", function () {
		times = {};
		renderAll();
	});

	// ---- times ----------------------------------------------------------------
	function addTime() {
		var v = timeInput.value;
		if (!/^\d{2}:\d{2}$/.test(v)) return;
		times[v] = true;
		timeInput.value = "";
		renderAll();
	}
	document.getElementById("add-time").addEventListener("click", addTime);
	timeInput.addEventListener("change", addTime);
	timeInput.addEventListener("keydown", function (e) {
		if (e.key === "Enter") {
			e.preventDefault();
			addTime();
		}
	});

	// ---- slot preview ----------------------------------------------------------
	function renderPreview() {
		var ds = sorted(dates);
		var ts = sorted(times);
		if (ds.length === 0) {
			preview.textContent =
				"Select date(s) to see the candidate slots.";
			return;
		}
		if (ts.length === 0) {
			preview.textContent = ds.length +
				" date(s) selected, no times — they all become midnight slots.";
			return;
		}
		var html = "<p>" + ds.length + " date(s) × " + ts.length +
			" time(s) = " + (ds.length * ts.length) + " slot(s)</p>";
		html += '<table class="slot-table"><tr><th>Date</th>';
		for (var j = 0; j < ts.length; j++) {
			html += "<th>" + ts[j] + "</th>";
		}
		html += "</tr>";
		for (var i = 0; i < ds.length; i++) {
			html += "<tr><td>" + ds[i] + "</td>";
			for (var k = 0; k < ts.length; k++) {
				html += "<td>" + ts[k] + "</td>";
			}
			html += "</tr>";
		}
		html += "</table>";
		preview.innerHTML = html;
	}

	// ---- timezone -----------------------------------------------------------
	// Pre-select the browser's zone in the timezone <select>. If the zone
	// is not among the server-rendered options, add it so the prefill is
	// preserved and the value still submits (validated server-side).
	try {
		var tzSel = document.getElementById("timezone");
		var localTz = Intl.DateTimeFormat().resolvedOptions().timeZone;
		// In edit mode the server already selected the poll's zone; only
		// prefill the browser zone when the server selected nothing. (A
		// select with no selected option still reports its first option
		// as .value, so test the attribute, not the value.)
		if (localTz && !tzSel.querySelector("option[selected]")) {
			if (tzSel.querySelector('option[value="' + localTz + '"]') === null) {
				var tzOpt = document.createElement("option");
				tzOpt.value = localTz;
				tzOpt.textContent = localTz.replace(/_/g, " ");
				// Insert at the alphabetical position, keeping the dropdown
				// sorted like the server-rendered options.
				var i = 0;
				while (i < tzSel.options.length && tzSel.options[i].value < localTz) {
					i++;
				}
				tzSel.insertBefore(tzOpt,
					i < tzSel.options.length ? tzSel.options[i] : null);
			}
			tzSel.value = localTz;
		}
	} catch (err) { /* very old browsers: no prefill */ }

	// ---- submit ----------------------------------------------------------------
	// The chip state serializes into date0..n / time0..m hidden fields; the
	// timezone travels via the <select> element.
	form.addEventListener("submit", function () {
		var old = form.querySelectorAll("[data-picker-input]");
		for (var i = 0; i < old.length; i++) old[i].remove();
		var ds = sorted(dates);
		var ts = sorted(times);
		for (var a = 0; a < ds.length; a++) {
			var dEl = document.createElement("input");
			dEl.type = "hidden";
			dEl.name = "date" + a;
			dEl.value = ds[a];
			dEl.setAttribute("data-picker-input", "1");
			form.appendChild(dEl);
		}
		for (var b = 0; b < ts.length; b++) {
			var tEl = document.createElement("input");
			tEl.type = "hidden";
			tEl.name = "time" + b;
			tEl.value = ts[b];
			tEl.setAttribute("data-picker-input", "1");
			form.appendChild(tEl);
		}
	});

	function renderAll() {
		renderCalendar();
		renderDateChips();
		renderTimeChips();
		renderPreview();
	}
	renderAll();
})();
