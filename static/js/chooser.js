// chooser.js — dynamic choice rows for the vote form: one label and/or
// one image per row. The image is uploaded to the server the moment it is
// dropped or picked (resized + re-encoded there); the row keeps the
// stored image's ID in a hidden field. Multi-file drops fill the dropped
// row first, then the next image-less rows, then new rows (never
// overwriting a filled one). Rows reorder by drag (the grip) or the
// up/down buttons — row order is the column order on the answer page.
// The submit stays disabled while any row has neither a label nor an
// image.
(function () {
	"use strict";

	const MAX = 20;
	const MAX_FILE = 10 * 1024 * 1024; // 10 MB — the server's per-file cap
	const OK_TYPES = "image/jpeg image/png image/webp image/gif image/bmp".split(
		" "
	);

	const container = document.getElementById("choice-rows");
	const addBtn = document.getElementById("add-choice");
	const countEl = document.getElementById("choice-count");
	const hintEl = document.getElementById("option-hint");
	const errEl = document.getElementById("upload-err");
	const formEl = document.getElementById("form");
	const submit = formEl ? formEl.querySelector('input[type="submit"]') : null;
	if (!container || !addBtn || !countEl) return;

	const t = (key) => (window.I18N && window.I18N.t && window.I18N.t[key]) || "";

	function rows() {
		return Array.prototype.slice.call(
			container.querySelectorAll(".choice-row")
		);
	}

	const textOf = (row) => {
		const inp = row.querySelector('input[type="text"]');
		return inp ? inp.value.trim() : "";
	};
	const slotOf = (row) => row.querySelector(".choice-slot");
	const slotFilled = (row) => slotOf(row).classList.contains("filled");
	const rowValid = (row) => textOf(row) !== "" || slotFilled(row);

	// The hidden fields travel with their row: choiceN (the label),
	// choiceIDN (the stable option ID on edit), choiceIMGN (the stored
	// image ID).
	function reindex() {
		rows().forEach(function (row, i) {
			const input = row.querySelector('input[type="text"]');
			if (input) input.name = "choice" + i;
			const hid = row.querySelector(
				'input[type="hidden"][name^="choiceID"]'
			);
			if (hid) hid.name = "choiceID" + i;
			const img = row.querySelector(
				'input[type="hidden"][name^="choiceIMG"]'
			);
			if (img) img.name = "choiceIMG" + i;
		});
	}

	function updateCount() {
		const n = rows().length;
		countEl.textContent = n + " / " + MAX;
		addBtn.disabled = n >= MAX;
	}

	// The submit gate: every row needs a label, an image, or both.
	function updateGate() {
		const ok = rows().length > 0 && rows().every(rowValid);
		if (submit) submit.disabled = !ok;
		if (hintEl) {
			if (ok) {
				hintEl.hidden = true;
				hintEl.classList.remove("err");
			} else {
				hintEl.hidden = false;
			}
		}
	}

	// The label is only required while the row has no image.
	function syncRowRequired(row) {
		const inp = row.querySelector('input[type="text"]');
		if (inp) inp.required = !slotFilled(row);
	}

	// ---- the image slot ---------------------------------------------------

	function fillSlot(row, id) {
		const slot = slotOf(row);
		const old = slot.querySelector(".choice-img");
		if (old) old.remove();
		const oldHidden = slot.querySelector(
			'input[type="hidden"][name^="choiceIMG"]'
		);
		if (oldHidden) oldHidden.remove();

		const img = document.createElement("img");
		img.className = "choice-img";
		img.src = "/vote-img/" + id + "/thumb";
		img.alt = "";
		img.width = 64;
		img.height = 64;
		const hid = document.createElement("input");
		hid.type = "hidden";
		hid.name = "choiceIMG";
		hid.value = id;
		slot.appendChild(img);
		slot.appendChild(hid);
		slot.classList.remove("uploading");
		slot.classList.add("filled");
		syncRowRequired(row);
	}

	function clearSlot(row) {
		const slot = slotOf(row);
		const img = slot.querySelector(".choice-img");
		if (img) img.remove();
		const hid = slot.querySelector('input[type="hidden"][name^="choiceIMG"]');
		if (hid) hid.remove();
		slot.classList.remove("filled", "uploading");
		syncRowRequired(row);
	}

	function showErr(key) {
		if (!errEl) return;
		errEl.textContent = t(key);
		errEl.hidden = !errEl.textContent;
	}
	const hideErr = () => {
		if (!errEl) return;
		errEl.textContent = "";
		errEl.hidden = true;
	};

	// ---- uploading --------------------------------------------------------
	// One round trip per file batch: POST /vote-img (multipart, field
	// "files"). The server decodes, resizes (256 + 1024) and re-encodes
	// each file and answers one result per file, in order. Pre-checks
	// (size, a known non-image MIME type) fail locally, the rest ride
	// the network. The planned targets mark their slots .uploading while
	// the request runs.
	let busy = false;

	// The slots a batch lands in: the dropped/picked row first (while
	// image-less), then the next image-less rows, then new ones (null at
	// the max).
	function planTargets(n, startRow) {
		const targets = [];
		let cursor = -1;
		for (let idx = 0; idx < n; idx++) {
			let target = null;
			if (idx === 0 && startRow && !slotFilled(startRow)) target = startRow;
			if (!target) {
				const all = rows();
				const from = idx === 0 ? 0 : cursor + 1;
				for (let i = from; i < all.length; i++) {
					if (!slotFilled(all[i])) {
						target = all[i];
						break;
					}
				}
			}
			if (!target) target = addRow(); // null at the max
			targets.push(target);
			if (target) cursor = rows().indexOf(target);
		}
		return targets;
	}

	function upload(fileList, startRow) {
		if (busy) return;
		const files = Array.prototype.slice.call(fileList || []);
		if (!files.length) return;

		const pre = new Array(files.length).fill("");
		files.forEach(function (f, i) {
			if (f.size > MAX_FILE) pre[i] = "form.fileTooLarge";
			else if (f.type && OK_TYPES.indexOf(f.type) < 0)
				pre[i] = "form.fileType";
		});
		const send = files.filter(function (f, i) {
			return !pre[i];
		});
		if (!send.length) {
			showErr(pre[0]);
			return;
		}

		const targets = planTargets(send.length, startRow);
		targets.forEach(function (tr) {
			if (tr) tr.classList.add("uploading");
		});
		busy = true;
		const fd = new FormData();
		send.forEach(function (f) {
			fd.append("files", f);
		});

		const done = function () {
			busy = false;
			targets.forEach(function (tr) {
				if (tr) tr.classList.remove("uploading");
			});
			reindex(); // fillSlot mints an indexless hidden name — index it now
			updateGate();
		};
		fetch("/vote-img", { method: "POST", body: fd })
			.then(function (r) {
				if (!r.ok) throw new Error("status " + r.status);
				return r.json();
			})
			.then(function (results) {
				let j = 0;
				let s = 0;
				let errKey = "";
				files.forEach(function (f, i) {
					if (pre[i]) {
						if (!errKey) errKey = pre[i];
						return;
					}
					const res = results[s++];
					if (!res || !res.id) {
						if (!errKey) errKey = "form.uploadFailed";
					} else if (targets[j]) {
						fillSlot(targets[j], res.id);
					}
					j++;
				});
				if (errKey) showErr(errKey);
				else hideErr();
			})
			.catch(function () {
				showErr("form.uploadFailed");
			})
			.then(done);
	}

	// The per-row picker: the slot's file input (the slot is a label
	// around it).
	container.addEventListener("change", function (e) {
		const fileInput = e.target;
		if (!fileInput.matches || !fileInput.matches(".choice-file")) return;
		const row = fileInput.closest(".choice-row");
		// input.files is a live list: snapshot it before the clear below
		// (which makes re-picking the same file re-fire the event), or the
		// snapshot would be empty by the time the upload reads it.
		const files = Array.prototype.slice.call(fileInput.files);
		fileInput.value = ""; // re-picking the same file re-fires change
		upload(files, row);
	});

	// The slot's corner x removes the image (the row keeps its label).
	container.addEventListener("click", function (e) {
		const x = e.target.closest && e.target.closest(".choice-slot-x");
		if (!x) return;
		e.preventDefault();
		e.stopPropagation();
		clearSlot(x.closest(".choice-row"));
		updateGate();
	});

	// ---- rows: add / remove / reorder -------------------------------------

	// addRow appends a fresh option row and returns it (the drop target
	// planning fills the row's slot; the Enter path focuses its input).
	function addRow() {
		if (rows().length >= MAX) return null;
		// Clone the server-rendered row (always in sync with the
		// template's markup) and strip its state.
		const src = container.querySelector(".choice-row");
		const row = src.cloneNode(true);
		row.querySelectorAll('input[type="hidden"]').forEach((el) => el.remove());
		const input = row.querySelector('input[type="text"]');
		if (input) input.value = "";
		const slot = row.querySelector(".choice-slot");
		if (slot) {
			slot
				.querySelectorAll(".choice-img")
				.forEach((el) => el.remove());
			slot.classList.remove("filled", "uploading");
		}
		container.appendChild(row);
		reindex();
		updateCount();
		return row;
	}

	function moveRow(row, delta) {
		const all = rows();
		const i = all.indexOf(row);
		const j = i + delta;
		if (i < 0 || j < 0 || j >= all.length) return;
		const ref = all[j];
		if (delta < 0) container.insertBefore(row, ref);
		else container.insertBefore(row, ref.nextSibling);
		reindex();
	}

	addBtn.addEventListener("click", function () {
		addRow();
	});

	container.addEventListener("click", function (e) {
		const rm = e.target.closest && e.target.closest(".choice-remove");
		if (rm) {
			if (rows().length <= 1) return;
			rm.closest(".choice-row").remove();
			reindex();
			updateCount();
			updateGate();
			return;
		}
		const mv = e.target.closest && e.target.closest(".choice-move");
		if (mv) {
			moveRow(
				mv.closest(".choice-row"),
				mv.getAttribute("data-dir") === "up" ? -1 : 1
			);
		}
	});

	container.addEventListener("keydown", function (e) {
		if (e.key !== "Enter") return;
		const input = e.target;
		if (!input.matches(".choice-row input[type=text]")) return;
		e.preventDefault();
		const next = addRow();
		if (next) {
			const text = next.querySelector('input[type="text"]');
			if (text) text.focus();
		}
	});

	// Typing keeps the gate + the per-row required flag in sync.
	container.addEventListener("input", function (e) {
		if (!e.target.matches(".choice-row input[type=text]")) return;
		syncRowRequired(e.target.closest(".choice-row"));
		updateGate();
	});

	// The drag reorder: the grip makes its row draggable (a plain
	// draggable row would swallow text selection in the label input).
	// The up/down buttons are the touch/keyboard path. The landing line
	// is nearest-midpoint over the whole container: it shows at the
	// dragged row's own position the moment the row is picked up, stays
	// put while the pointer sits over the dragged row itself, the row
	// gaps, or off the list, and flips to the neighbouring zone as soon
	// as the pointer crosses a midpoint — no dead zones, no delay, gone
	// on release. The geometry (the dragged row's band + every other
	// row's midpoint) is measured once on pickup and re-measured on
	// scroll/resize: re-reading the rows' rects on every pointer event
	// forces a layout per dragover, and the line lags behind (and a
	// clear+re-add classList churn flickers it) — that was the
	// "disappears while moving" judder.
	let dragRow = null;
	let dragGeom = null; // { top, bottom, mids: [{ row, mid }] }
	let lineMark = null; // the row + side the line is shown on
	const hasFiles = (e) =>
		e.dataTransfer &&
		Array.prototype.indexOf.call(e.dataTransfer.types, "Files") >= 0;

	function measureDrag() {
		const band = dragRow.getBoundingClientRect();
		const mids = [];
		rows().forEach(function (r) {
			if (r === dragRow) return;
			const rect = r.getBoundingClientRect();
			mids.push({ row: r, mid: rect.top + rect.height / 2 });
		});
		dragGeom = { top: band.top, bottom: band.bottom, mids };
	}

	// The row the pointer lands at: the one whose midpoint is nearest
	// (before/after by which side of it the pointer sits). Pure math
	// over the pickup measurement; the dragged row itself is never a
	// target.
	function dropTarget(y) {
		let best = null;
		let bestD = Infinity;
		for (let i = 0; i < dragGeom.mids.length; i++) {
			const mid = dragGeom.mids[i].mid;
			const d = Math.abs(y - mid);
			if (d < bestD) {
				bestD = d;
				best = { row: dragGeom.mids[i].row, before: y < mid };
			}
		}
		return best;
	}

	// Show the line only while its target row or side changes —
	// re-toggling the same class on every pointer event is pure
	// layout churn.
	function applyLine(t) {
		if (!t) return;
		if (lineMark && lineMark.row === t.row && lineMark.before === t.before)
			return;
		if (lineMark)
			lineMark.row.classList.remove(
				lineMark.before ? "drop-before" : "drop-after"
			);
		t.row.classList.add(t.before ? "drop-before" : "drop-after");
		lineMark = t;
	}

	function clearDropMarks() {
		rows().forEach(function (r) {
			r.classList.remove("drop-before", "drop-after");
		});
		lineMark = null;
	}

	container.addEventListener("mousedown", function (e) {
		const grip = e.target.closest && e.target.closest(".choice-grip");
		if (!grip) return;
		grip.closest(".choice-row").draggable = true;
	});
	document.addEventListener("mouseup", function () {
		rows().forEach(function (r) {
			r.draggable = false;
		});
	});
	container.addEventListener("dragstart", function (e) {
		const row = e.target.closest && e.target.closest(".choice-row");
		if (!row) return;
		dragRow = row;
		row.classList.add("dragging");
		measureDrag();
		// The line appears at the row's own position on pickup (after the
		// row above it, before the one below it, if there is none).
		const all = rows();
		const i = all.indexOf(row);
		if (i > 0) applyLine({ row: all[i - 1], before: false });
		else if (all[i + 1]) applyLine({ row: all[i + 1], before: true });
		if (e.dataTransfer) {
			e.dataTransfer.effectAllowed = "move";
			try {
				e.dataTransfer.setData("text/plain", "");
			} catch (_) {
				/* jsdom */
			}
		}
	});
	container.addEventListener("dragover", function (e) {
		if (!dragRow && !hasFiles(e)) return;
		e.preventDefault();
		if (!dragRow) return;
		if (e.dataTransfer) e.dataTransfer.dropEffect = "move";
		const y = e.clientY;
		// Over the dragged row's own slot the line stays where it is
		// (the row is never its own target, so a computed target would
		// just erase it — the pickup line sits in this gap, so "stay
		// put" reads as "no move yet").
		if (y >= dragGeom.top && y <= dragGeom.bottom) return;
		applyLine(dropTarget(y));
	});
	container.addEventListener("drop", function (e) {
		e.preventDefault();
		if (dragRow) {
			const t = dropTarget(e.clientY);
			clearDropMarks();
			if (t) {
				container.insertBefore(
					dragRow,
					t.before ? t.row : t.row.nextSibling
				);
				reindex();
			}
			dragRow.classList.remove("dragging");
			dragRow = null;
			dragGeom = null;
			return;
		}
		container.classList.remove("drop-target");
		upload(
			e.dataTransfer ? e.dataTransfer.files : [],
			e.target.closest ? e.target.closest(".choice-row") : null
		);
	});
	container.addEventListener("dragend", function () {
		if (dragRow) {
			dragRow.classList.remove("dragging");
			dragRow = null;
			dragGeom = null;
		}
		clearDropMarks();
		container.classList.remove("drop-target");
	});
	container.addEventListener("dragenter", function (e) {
		if (dragRow || !hasFiles(e)) return;
		container.classList.add("drop-target");
	});
	container.addEventListener("dragleave", function (e) {
		if (e.target === container) container.classList.remove("drop-target");
	});
	// A page scroll or resize mid-drag makes the pickup measurement
	// stale — re-measure (one frame of rect reads, only while a row is
	// dragged).
	window.addEventListener(
		"scroll",
		function () {
			if (dragRow) measureDrag();
		},
		{ passive: true, capture: true }
	);
	window.addEventListener("resize", function () {
		if (dragRow) measureDrag();
	});

	// ---- a click on the greyed submit flashes the gate hint ---------------
	// (a disabled input swallows its own click — the CSS pointer-events
	// rule lets it land on the form, so the handler hit-tests the
	// coordinates).
	if (submit && hintEl) {
		hintEl.addEventListener("animationend", function () {
			hintEl.classList.remove("err");
		});
		formEl.addEventListener("click", function (e) {
			if (!submit.disabled) return;
			const b = submit.getBoundingClientRect();
			if (
				e.clientX >= b.left &&
				e.clientX <= b.right &&
				e.clientY >= b.top &&
				e.clientY <= b.bottom
			) {
				hintEl.classList.remove("err");
				void hintEl.offsetWidth;
				hintEl.classList.add("err");
			}
		});
	}

	// The mode checkboxes and the voting type disable each other:
	//  - single-choice: yes/no and yes/maybe/no are invalid (a voter who
	//    may pick only one option can neither mark no nor hedge with a
	//    maybe), must-answer is invalid, and the default type is yes;
	//  - must-answer: yes is invalid (it would force a yes on every
	//    option), single-choice is invalid, and the default type is
	//    yes/no.
	// A selection that lands on a greyed radio moves to the mode's
	// default. The server re-checks every combination.
	var sc = document.getElementById("single-choice");
	var scNote = document.getElementById("single-choice-note");
	var scWarn = document.getElementById("single-choice-warn"); // edit form only
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
			if (scWarn) scWarn.hidden = !sc.checked; // wipe-warning while on
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

	// ---- character counters ------------------------------------------------
	// The same counters dater.js owns on the poll form (this page loads
	// only this script): title/email/description have server-side caps
	// (main.go); mirror them as maxlength and show a live "n / max"
	// count. Count by code points ([...s]) to match the server's
	// utf8.RuneCountInString, so one emoji reads as one character, not
	// two.
	var fieldCaps = { title: 100, email: 256, description: 5000 };
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

	reindex();
	updateCount();
	updateGate();
})();
