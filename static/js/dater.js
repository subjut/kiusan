// dater.js — date/time picker for the meeting form (vanilla, no deps).
//
// Model: candidate slots are per date — each selected date carries its
// own set of times, and a date without times is a midnight slot. The
// "add to all dates" input applies one time to every selected date at
// once. All state is plain YYYY-MM-DD / HH:MM strings, so there are no
// Date-object timezone pitfalls — "today" comes from the user's browser
// on purpose, since the earliest selectable date should follow the user,
// not the server.

(function () {
	"use strict";

	var form = document.getElementById("form");
	if (!form) return;

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

	// ---- state -----------------------------------------------------------
	// "YYYY-MM-DD" -> { "HH:MM" -> true }
	var dateTimes = {};

	// When the form edits an existing poll, the server seeds the picker
	// via data-slots (comma-separated "YYYY-MM-DD[THH:MM]" slots, one per
	// candidate slot of the poll).
	(form.getAttribute("data-slots") || "").split(",").forEach(function (v) {
		if (!v) return;
		var d = v, t = "", i = v.indexOf("T");
		if (i >= 0) {
			d = v.slice(0, i);
			t = v.slice(i + 1);
		}
		if (!dateTimes[d]) dateTimes[d] = {};
		if (t) dateTimes[d][t] = true;
	});

	var dateChips = document.getElementById("date-chips");
	var dateTimesEl = document.getElementById("date-times");
	var timeInput = document.getElementById("time-input");
	var preview = document.getElementById("slot-preview");
	var grid = document.getElementById("cal-grid");
	var calTitle = document.getElementById("cal-title");

	// Weekday + month labels come from the viewer's locale (locale.js):
	// the picker is a wall-calendar, so its dates are formatted without a
	// timeZone — the y/m/d parts stay the ones the widget was built from.
	function firstDow() {
		// The locale's first weekday (0 = Sunday), best effort: some
		// browsers don't report resolvedOptions().firstDay.
		try {
			var fd = new Intl.DateTimeFormat(Locale.get())
				.resolvedOptions().firstDay;
			if (typeof fd === "number" && fd >= 0 && fd <= 6) return fd;
		} catch (e) { /* no firstDay: Sunday */ }
		return 0;
	}

	function weekdayName(w) { // w: 0 = Sunday
		// 2024-01-01 was a Monday.
		return Locale.dtf({ weekday: "narrow" })
			.format(new Date(2024, 0, 1 + ((w + 6) % 7)));
	}

	function monthTitle(y, m) {
		return Locale.dtf({ month: "long", year: "numeric" })
			.format(new Date(y, m, 1));
	}

	function chipDate(d) { // "YYYY-MM-DD" -> locale short date
		var t = d.split("-");
		return Locale.dtf({ day: "numeric", month: "short", year: "numeric" })
			.format(new Date(+t[0], +t[1] - 1, +t[2]));
	}

	function timeLabel(t) { // "HH:MM" -> locale 12/24h time
		var p = t.split(":");
		return Locale.dtf({ hour: "numeric", minute: "2-digit" })
			.format(new Date(2000, 0, 1, +p[0], +p[1]));
	}

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
		for (var d in dateTimes) { base = d; break; }
		var t = base.split("-");
		return { y: +t[0], m: +t[1] - 1 }; // m: 0-based
	})();

	function renderCalendar() {
		var min = localToday();
		calTitle.textContent = monthTitle(view.y, view.m);

		var dow0 = firstDow();
		var html = "<tr>";
		for (var n = 0; n < 7; n++) {
			html += '<th scope="col">' + weekdayName((dow0 + n) % 7) + "</th>";
		}
		html += "</tr>";

		var lead = (new Date(view.y, view.m, 1).getDay() - dow0 + 7) % 7;
		var dim = daysInMonth(view.y, view.m);
		// A month always fits in six week-rows; cells outside the month
		// are left empty.
		for (var w = 0; w < 6; w++) {
			html += "<tr>";
			for (var col = 0; col < 7; col++) {
				var idx = w * 7 + col - lead + 1;
				if (idx < 1 || idx > dim) {
					html += "<td></td>";
					continue;
				}
				var dateStr = iso(view.y, view.m, idx);
				var past = dateStr < min;
				var cls = "day";
				if (dateTimes[dateStr]) cls += " selected";
				html += '<td><button type="button" class="' + cls + '"' +
					(past ? " disabled" : "") + ' data-date="' + dateStr +
					'" title="' + chipDate(dateStr) + '">' + idx + "</button></td>";
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
		if (dateTimes[d]) {
			delete dateTimes[d];
		} else {
			dateTimes[d] = {};
		}
		renderAll();
	});

	// ---- chips + per-date times -------------------------------------------
	function sorted(obj) {
		return Object.keys(obj).sort();
	}

	function timeChip(d, tm) {
		var label = timeLabel(tm);
		return '<span class="chip">' + label +
			'<button type="button" class="chip-x" data-date="' + d +
			'" data-time="' + tm + '" title="' +
			fill(t("dater.removeTime"), { time: label, date: chipDate(d) }) +
			'">✕</button></span>';
	}

	function renderDateChips() {
		dateChips.innerHTML = sorted(dateTimes).map(function (d) {
			return '<span class="chip">' + chipDate(d) +
				'<button type="button" class="chip-x" data-date="' + d +
				'" title="' + fill(t("dater.removeDate"), { date: chipDate(d) }) +
				'">✕</button></span>';
		}).join("");
	}

	function renderDateTimes() {
		dateTimesEl.innerHTML = sorted(dateTimes).map(function (d) {
			var ts = sorted(dateTimes[d]);
			var chips = ts.map(function (t) {
				return timeChip(d, t);
			}).join("");
			return '<div class="date-times-row">' +
				'<span class="date-times-label">' + chipDate(d) + "</span>" +
				'<span class="chips">' + chips + "</span>" +
				'<input type="time" class="date-time-input" data-date="' + d + '">' +
				'<button type="button" class="btn-secondary date-time-add" data-date="' + d + '">' +
				t("dater.add") + "</button>" +
				"</div>";
		}).join("");
	}

	dateChips.addEventListener("click", function (e) {
		var x = e.target.closest(".chip-x");
		if (!x) return;
		delete dateTimes[x.getAttribute("data-date")];
		renderAll();
	});

	dateTimesEl.addEventListener("click", function (e) {
		var x = e.target.closest(".chip-x");
		if (x) {
			var d = x.getAttribute("data-date");
			if (dateTimes[d]) {
				delete dateTimes[d][x.getAttribute("data-time")];
			}
			renderAll();
			return;
		}
		var add = e.target.closest(".date-time-add");
		if (!add) return;
		var row = add.closest(".date-times-row");
		var input = row ? row.querySelector(".date-time-input") : null;
		if (input && /^\d{2}:\d{2}$/.test(input.value)) {
			var d2 = add.getAttribute("data-date");
			if (dateTimes[d2]) dateTimes[d2][input.value] = true;
			renderAll();
		}
	});
	// Enter in a per-date time input adds that time to its date (same as
	// clicking that row's "Add"), for parity with the "add to all dates" input.
	dateTimesEl.addEventListener("keydown", function (e) {
		if (e.key !== "Enter") return;
		var input = e.target.closest(".date-time-input");
		if (!input) return;
		e.preventDefault();
		var d = input.getAttribute("data-date");
		if (dateTimes[d] && /^\d{2}:\d{2}$/.test(input.value)) {
			dateTimes[d][input.value] = true;
			renderAll();
		}
	});

	document.getElementById("clear-dates").addEventListener("click", function () {
		dateTimes = {};
		renderAll();
	});
	document.getElementById("clear-times").addEventListener("click", function () {
		for (var d in dateTimes) dateTimes[d] = {};
		renderAll();
	});

	// ---- times ----------------------------------------------------------------
	// "Add to all dates": one time, applied to every selected date at once.
	// With no dates selected the input keeps its value (nothing to apply).
	function addTimeToAll() {
		var v = timeInput.value;
		if (!/^\d{2}:\d{2}$/.test(v)) return;
		var any = false;
		for (var d in dateTimes) {
			dateTimes[d][v] = true;
			any = true;
		}
		if (any) {
			timeInput.value = "";
			renderAll();
		}
	}
	document.getElementById("add-time").addEventListener("click", addTimeToAll);
	timeInput.addEventListener("keydown", function (e) {
		if (e.key === "Enter") {
			e.preventDefault();
			addTimeToAll();
		}
	});

	// ---- slot preview ----------------------------------------------------------
	function renderPreview() {
		var ds = sorted(dateTimes);
		if (ds.length === 0) {
			preview.textContent = t("dater.previewEmpty");
			return;
		}
		var timed = 0, midnights = 0;
		var lines = [];
		for (var i = 0; i < ds.length; i++) {
			var d = ds[i];
			var ts = sorted(dateTimes[d]);
			if (ts.length === 0) {
				midnights++;
				lines.push("<p><b>" + chipDate(d) + "</b> — " +
					t("dater.previewMidnight") + "</p>");
			} else {
				timed += ts.length;
				lines.push("<p><b>" + chipDate(d) + "</b> — " +
					ts.map(timeLabel).join(", ") + "</p>");
			}
		}
		var html = lines.join("");
		var key = midnights ? "dater.previewSummary"
			: "dater.previewSummaryNoMidnight";
		html += "<p>" + fill(t(key),
			{ timed: timed, midnight: midnights, total: timed + midnights }) +
			"</p>";
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
			// Compare option values directly (not via a built selector
			// string), so a zone name with quotes can't break the selector.
			var hasOpt = false;
			for (var k = 0; k < tzSel.options.length; k++) {
				if (tzSel.options[k].value === localTz) {
					hasOpt = true;
					break;
				}
			}
			if (!hasOpt) {
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

	// ---- character counters ------------------------------------------------
	// title/email/description have server-side caps (main.go); mirror them
	// as maxlength and show a live "n / max" count. Count by code points
	// ([...s]) to match the server's utf8.RuneCountInString, so one emoji
	// reads as one character, not two.
	var fieldCaps = { title: 100, email: 320, description: 5000 };
	Object.keys(fieldCaps).forEach(function (id) {
		var el = document.getElementById(id);
		var label = document.getElementById(id + "-count");
		if (!el || !label) return;
		function update() {
			label.textContent = [...el.value].length + " / " + fieldCaps[id];
		}
		el.addEventListener("input", update);
		update(); // show the count for prefilled (edit-mode) values
	});

	// ---- submit ----------------------------------------------------------------
	// The picker state serializes into slot0..n hidden fields, one per
	// candidate slot: "YYYY-MM-DD" (midnight) or "YYYY-MM-DDTHH:MM",
	// dates and times ascending.
	form.addEventListener("submit", function () {
		var old = form.querySelectorAll("[data-picker-input]");
		for (var i = 0; i < old.length; i++) old[i].remove();
		var n = 0;
		sorted(dateTimes).forEach(function (d) {
			var emit = function (value) {
				var el = document.createElement("input");
				el.type = "hidden";
				el.name = "slot" + n;
				el.value = value;
				el.setAttribute("data-picker-input", "1");
				form.appendChild(el);
				n++;
			};
			var ts = sorted(dateTimes[d]);
			if (ts.length === 0) {
				emit(d);
			} else {
				ts.forEach(function (t) { emit(d + "T" + t); });
			}
		});
	});

	function renderAll() {
		renderCalendar();
		renderDateChips();
		renderDateTimes();
		renderPreview();
	}
	Locale.onChange(renderAll); // the "Display" dropdown re-renders the picker
	renderAll();
})();
