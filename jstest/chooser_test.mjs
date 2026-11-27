// chooser.js — the vote form's option rows: one label and/or one image
// per row, the image uploaded on drop/pick (the server resizes + stores
// it), the multi-file target planning, the drag/button reordering, the
// submit gate (every row needs a label, an image, or both), the mode
// checkbox interlock, and the character counters.
import test from "node:test";
import assert from "node:assert/strict";
import { boot, settled, click } from "./harness.mjs";

const I18N = {
	lang: "en",
	t: {
		"form.fileTooLarge": "Image too large (max 10 MB)",
		"form.fileType": "Not a supported image format",
		"form.uploadFailed": "Upload failed — please try again",
	},
};

function choiceRow(i, { text = "", img = null, id = "" } = {}) {
	const filled = img
		? `<img class="choice-img" src="/vote-img/${img}/thumb" alt="" width="64" height="64">
<input type="hidden" name="choiceIMG${i}" value="${img}">`
		: `<span class="choice-slot-icon" aria-hidden="true"></span>`;
	const cls = img ? "choice-slot filled" : "choice-slot";
	return `<div class="choice-row">
<span class="choice-grip" aria-hidden="true"></span>
<label class="${cls}" title="Upload image">
<input type="file" class="choice-file" accept="image/jpeg,image/png" multiple hidden>${filled}
<button type="button" class="choice-slot-x" title="Remove image" aria-label="Remove image">&times;</button>
</label>
<input type="text" name="choice${i}" value="${text}" maxlength="100"${img ? "" : " required"}>
<input type="hidden" name="choiceID${i}" value="${id}">
<button type="button" class="choice-move" data-dir="up" title="Up" aria-label="Up">&uarr;</button>
<button type="button" class="choice-move" data-dir="down" title="Down" aria-label="Down">&darr;</button>
<button type="button" class="choice-remove" title="Remove" aria-label="Remove">&times;</button>
</div>`;
}

function fixture({ rows = [{ text: "" }], checked = "yes-no" } = {}) {
	return `<form id="form" method="POST" action="/vote-form">
<div class="form-field"><input type="text" id="title" name="title" maxlength="100"><span class="char-count" id="title-count"></span></div>
<div class="form-field"><input type="text" id="email" name="email" maxlength="256"><span class="char-count" id="email-count"></span></div>
<div class="form-field"><textarea id="description" name="description" maxlength="5000"></textarea><span class="char-count" id="description-count"></span></div>
<div class="form-field">
<input type="checkbox" id="single-choice" name="single-choice" value="true">
<p class="form-note" id="single-choice-note" hidden></p>
<p class="form-note" id="single-choice-warn" hidden></p>
</div>
<div class="form-field">
<input type="checkbox" id="must-answer" name="must-answer" value="true">
<p class="form-note" id="must-answer-note" hidden></p>
</div>
<fieldset class="vote-type-picker">
<input type="radio" id="votetype-yes" name="votetype" value="yes">
<input type="radio" id="votetype-yes-no" name="votetype" value="yes-no"${checked === "yes-no" ? " checked" : ""}>
<input type="radio" id="votetype-yes-maybe-no" name="votetype" value="yes-maybe-no"${checked === "yes-maybe-no" ? " checked" : ""}>
<input type="radio" id="votetype-freetext" name="votetype" value="freetext"${checked === "freetext" ? " checked" : ""}>
</fieldset>
<fieldset class="choices-picker">
<div id="choice-rows">
${rows.map((r, i) => choiceRow(i, r)).join("\n")}
</div>
<p class="form-note must-hint option-hint" id="option-hint">Every option needs a label, an image, or both.</p>
<p class="form-note upload-err" id="upload-err" hidden></p>
<button type="button" class="btn-secondary" id="add-choice">Add option</button>
<span class="choice-count" id="choice-count"></span>
</fieldset>
<input type="submit" value="Create">
</form>`;
}

async function page(opts = {}, fetchStub) {
	const w = boot(fixture(opts), {
		scripts: ["chooser.js"],
		i18n: I18N,
		fetch: fetchStub,
	});
	await settled(w);
	return w;
}

const rows = (w) => [...w.document.querySelectorAll("#choice-rows .choice-row")];
const slot = (row) => row.querySelector(".choice-slot");
const text = (row) => row.querySelector('input[type="text"]');
const submit = (w) => w.document.querySelector('input[type="submit"]');
const hint = (w) => w.document.getElementById("option-hint");
const errEl = (w) => w.document.getElementById("upload-err");

function makeFile(w, name, type, size = 1024) {
	const buf = new ArrayBuffer(size);
	return new w.File([buf], name, { type });
}

// uploadFetch stubs /vote-img: records the request count, answers one
// result per file (ids from results, a falsy entry yields an error
// result). The request body (a jsdom FormData) is not inspected — the
// slot fills below prove what was sent.
function uploadFetch(results) {
	const fn = async function (url) {
		fn.calls.push(url);
		if (url === "/vote-img") {
			return { ok: true, json: async () => results };
		}
		return { ok: false, json: async () => ({}) };
	};
	fn.calls = [];
	return fn;
}

function dropFiles(w, target, files) {
	const ev = new w.MouseEvent("drop", { bubbles: true, cancelable: true, clientY: 0 });
	ev.dataTransfer = { files, types: ["Files"] };
	target.dispatchEvent(ev);
}

test("the submit gate stays down until every row has a label or an image", async (t) => {
	const w = await page({ rows: [{ text: "" }, { text: "" }] });
	assert.ok(submit(w).disabled);
	assert.ok(!hint(w).hidden);
	// typing lifts the gate for that row
	text(rows(w)[0]).value = "Pizza";
	text(rows(w)[0]).dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.ok(submit(w).disabled); // the second row is still empty
	text(rows(w)[1]).value = "Sushi";
	text(rows(w)[1]).dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.ok(!submit(w).disabled);
	assert.ok(hint(w).hidden);
	// emptying a label re-locks while the row has no image
	text(rows(w)[0]).value = "";
	text(rows(w)[0]).dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.ok(submit(w).disabled);
	assert.ok(!hint(w).hidden);
});

test("an uploaded image lifts the gate and drops the label's required flag", async (t) => {
	const w = await page({ rows: [{ text: "" }] }, uploadFetch([{ id: "img-aaaa1111", w: 8, h: 8 }]));
	const row = rows(w)[0];
	assert.ok(text(row).required);
	dropFiles(w, row, [makeFile(w, "a.png", "image/png")]);
	await new Promise((res) => w.setTimeout(res, 0));
	assert.ok(slot(row).classList.contains("filled"));
	assert.ok(!slot(row).classList.contains("uploading"));
	const img = slot(row).querySelector(".choice-img");
	assert.equal(img.src, new w.URL("/vote-img/img-aaaa1111/thumb", "https://kiusan.test/").href);
	const hid = slot(row).querySelector('input[type="hidden"][name="choiceIMG0"]');
	assert.equal(hid.value, "img-aaaa1111");
	assert.ok(!text(row).required); // the label is optional while the image sits
	assert.ok(!submit(w).disabled);
	assert.ok(hint(w).hidden);
	assert.ok(errEl(w).hidden);
});

test("the slot's x removes the image (the row keeps its label)", async (t) => {
	const w = await page({ rows: [{ text: "Pizza", img: "img-bbbb2222" }] });
	const row = rows(w)[0];
	assert.ok(slot(row).classList.contains("filled"));
	click(w, slot(row).querySelector(".choice-slot-x"));
	assert.ok(!slot(row).classList.contains("filled"));
	assert.ok(!slot(row).querySelector(".choice-img"));
	assert.ok(!slot(row).querySelector('input[type="hidden"][name^="choiceIMG"]'));
	assert.equal(text(row).value, "Pizza");
	assert.ok(text(row).required); // back to label-required
	assert.ok(!submit(w).disabled); // the label still counts
});

test("add, remove, and move rows reindex the fields", async (t) => {
	const w = await page({ rows: [{ text: "A" }] });
	click(w, w.document.getElementById("add-choice"));
	let r = rows(w);
	assert.equal(r.length, 2);
	assert.equal(r[0].querySelector('input[type="text"]').name, "choice0");
	assert.equal(r[1].querySelector('input[type="text"]').name, "choice1");
	// the server-rendered row keeps its stable ID; a JS-added row carries
	// none (the server mints one on submit)
	assert.equal(r[0].querySelector('input[type="hidden"]').name, "choiceID0");
	assert.equal(r[1].querySelector('input[type="hidden"]'), null);
	assert.equal(w.document.getElementById("choice-count").textContent, "2 / 20");

	// moving the first row down swaps the order (and the names with it)
	click(w, r[0].querySelector('[data-dir="down"]'));
	r = rows(w);
	assert.equal(text(r[0]).value, "");
	assert.equal(text(r[1]).value, "A");
	assert.equal(text(r[1]).name, "choice1");
	assert.equal(text(r[0]).name, "choice0");

	// removing the now-first row reindexes the survivor
	click(w, r[0].querySelector(".choice-remove"));
	r = rows(w);
	assert.equal(r.length, 1);
	assert.equal(text(r[0]).value, "A");
	assert.equal(text(r[0]).name, "choice0");
	assert.equal(w.document.getElementById("choice-count").textContent, "1 / 20");
});

test("the last row cannot be removed", async (t) => {
	const w = await page({ rows: [{ text: "Only" }] });
	click(w, rows(w)[0].querySelector(".choice-remove"));
	assert.equal(rows(w).length, 1);
});

test("entering the last field adds a row and focuses it", async (t) => {
	const w = await page({ rows: [{ text: "" }] });
	const inp = text(rows(w)[0]);
	inp.dispatchEvent(new w.KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
	assert.equal(rows(w).length, 2);
	assert.equal(w.document.activeElement, text(rows(w)[1]));
});

test("a multi-file drop fills the dropped row first, then new rows", async (t) => {
	const w = await page({ rows: [{ text: "" }] }, uploadFetch([
		{ id: "img-one111111", w: 4, h: 4 },
		{ id: "img-two222222", w: 4, h: 4 },
	]));
	const row = rows(w)[0];
	dropFiles(w, row, [makeFile(w, "1.png", "image/png"), makeFile(w, "2.png", "image/png")]);
	await new Promise((res) => w.setTimeout(res, 0));
	const r = rows(w);
	assert.equal(r.length, 2); // the second file opened a new row
	assert.equal(slot(r[0]).querySelector('input[type="hidden"]').value, "img-one111111");
	assert.equal(slot(r[1]).querySelector('input[type="hidden"]').value, "img-two222222");
	// the hidden image fields carry their reindexed row positions
	assert.ok(r[0].querySelector('input[name="choiceIMG0"]'));
	assert.ok(r[1].querySelector('input[name="choiceIMG1"]'));
	assert.ok(!submit(w).disabled);
});

test("an oversize file is rejected before any request", async (t) => {
	const fetchStub = uploadFetch([]);
	const w = await page({ rows: [{ text: "" }] }, fetchStub);
	const big = new w.File([new ArrayBuffer(11 * 1024 * 1024)], "big.png", { type: "image/png" });
	dropFiles(w, rows(w)[0], [big]);
	await new Promise((res) => w.setTimeout(res, 0));
	assert.equal(fetchStub.calls.length, 0);
	assert.ok(!errEl(w).hidden);
	assert.equal(errEl(w).textContent, "Image too large (max 10 MB)");
	assert.ok(submit(w).disabled);
});

test("a file of an unsupported type is rejected before any request", async (t) => {
	const fetchStub = uploadFetch([]);
	const w = await page({ rows: [{ text: "" }] }, fetchStub);
	dropFiles(w, rows(w)[0], [makeFile(w, "doc.pdf", "application/pdf")]);
	await new Promise((res) => w.setTimeout(res, 0));
	assert.equal(fetchStub.calls.length, 0);
	assert.ok(!errEl(w).hidden);
	assert.equal(errEl(w).textContent, "Not a supported image format");
});

test("a server-side rejection maps onto the upload-failed message", async (t) => {
	const w = await page({ rows: [{ text: "" }] }, uploadFetch([{}]));
	dropFiles(w, rows(w)[0], [makeFile(w, "a.png", "image/png")]);
	await new Promise((res) => w.setTimeout(res, 0));
	assert.ok(!errEl(w).hidden);
	assert.equal(errEl(w).textContent, "Upload failed — please try again");
	assert.ok(!rows(w)[0].querySelector(".choice-img"));
});

test("a failed request maps onto the upload-failed message", async (t) => {
	const w = await page(
		{ rows: [{ text: "" }] },
		async () => { throw new Error("network down"); }
	);
	dropFiles(w, rows(w)[0], [makeFile(w, "a.png", "image/png")]);
	await new Promise((res) => w.setTimeout(res, 0));
	assert.ok(!errEl(w).hidden);
	assert.equal(errEl(w).textContent, "Upload failed — please try again");
});

test("the file picker's change event uploads into its row", async (t) => {
	const fetchStub = uploadFetch([{ id: "img-pick9999", w: 2, h: 2 }]);
	const w = await page({ rows: [{ text: "A" }, { text: "B" }] }, fetchStub);
	const inp = rows(w)[1].querySelector(".choice-file");
	// a live FileList, like the browser's: the list the handler reads
	// reflects the input, so the handler's own clear empties it too.
	// The snapshot must be taken before the clear, or nothing uploads.
	const live = [makeFile(w, "p.png", "image/png")];
	Object.defineProperty(inp, "files", { get: () => live, configurable: true });
	Object.defineProperty(inp, "value", {
		get: () => "",
		set(v) { if (v === "") live.length = 0; },
		configurable: true,
	});
	inp.dispatchEvent(new w.Event("change", { bubbles: true }));
	await new Promise((res) => w.setTimeout(res, 0));
	assert.equal(fetchStub.calls.length, 1);
	const picked = rows(w)[1].querySelector('.choice-slot img.choice-img');
	assert.equal(picked && picked.getAttribute("src"), "/vote-img/img-pick9999/thumb");
});

test("dragging the grip reorders the rows and their fields", async (t) => {
	const w = await page({ rows: [{ text: "A" }, { text: "B" }, { text: "C" }] });
	const [a, b, c] = rows(w);
	// mousedown on the grip makes the row draggable
	a.querySelector(".choice-grip").dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true }));
	assert.ok(a.draggable);
	const start = new w.Event("dragstart", { bubbles: true, cancelable: true });
	start.dataTransfer = { types: [] };
	a.dispatchEvent(start);
	assert.ok(a.classList.contains("dragging"));
	// the landing line shows at the dragged row's own position on pickup
	assert.ok(b.classList.contains("drop-before"));
	assert.ok(!b.classList.contains("drop-after"));
	// over the dragged row's own slot the line stays put (the row is never
	// its own target, so a recompute would just erase the pickup line)
	const ownOver = new w.MouseEvent("dragover", { bubbles: true, cancelable: true, clientY: 0 });
	ownOver.dataTransfer = { types: [] };
	a.dispatchEvent(ownOver);
	assert.ok(b.classList.contains("drop-before"));
	assert.ok(!b.classList.contains("drop-after"));
	assert.ok(!a.classList.contains("drop-before") && !a.classList.contains("drop-after"));
	// over the second row, in its lower half: the drop-after mark
	const over = new w.MouseEvent("dragover", { bubbles: true, cancelable: true, clientY: 1 });
	over.dataTransfer = { types: [] };
	b.dispatchEvent(over);
	assert.ok(b.classList.contains("drop-after"));
	// the row drag carries a "move" drop effect (cursor feedback)
	assert.equal(over.dataTransfer.dropEffect, "move");
	// the line tracks the pointer over the container itself (the gaps
	// between the rows — no dead zones) and flips sides as soon as the
	// pointer crosses a midpoint
	const gapOver = new w.MouseEvent("dragover", { bubbles: true, cancelable: true, clientY: -1 });
	gapOver.dataTransfer = { types: [] };
	w.document.getElementById("choice-rows").dispatchEvent(gapOver);
	assert.ok(b.classList.contains("drop-before"));
	assert.ok(!b.classList.contains("drop-after"));
	b.dispatchEvent(over); // back to the lower half
	assert.ok(b.classList.contains("drop-after"));
	// dropping there moves A behind B
	const drop = new w.MouseEvent("drop", { bubbles: true, cancelable: true, clientY: 1 });
	drop.dataTransfer = { types: [] };
	b.dispatchEvent(drop);
	let r = rows(w);
	assert.equal([...r.map((x) => text(x).value)].join(","), "B,A,C");
	assert.ok(!a.classList.contains("dragging"));
	assert.ok(!b.classList.contains("drop-after"));
	// the fields travel with their rows
	assert.equal(text(r[1]).name, "choice1");
	// in the upper half, A lands in front of B again
	a.querySelector(".choice-grip").dispatchEvent(new w.MouseEvent("mousedown", { bubbles: true }));
	const start2 = new w.Event("dragstart", { bubbles: true, cancelable: true });
	start2.dataTransfer = { types: [] };
	r[1].dispatchEvent(start2);
	const drop2 = new w.MouseEvent("drop", { bubbles: true, cancelable: true, clientY: -1 });
	drop2.dataTransfer = { types: [] };
	r[0].dispatchEvent(drop2);
	r = rows(w);
	assert.equal([...r.map((x) => text(x).value)].join(","), "A,B,C");
	// mouseup clears the draggable flag
	w.document.dispatchEvent(new w.MouseEvent("mouseup"));
	assert.ok(!r[0].draggable);
});

test("a file drag over the form marks the drop target", async (t) => {
	const w = await page({ rows: [{ text: "" }] }, uploadFetch([]));
	const container = w.document.getElementById("choice-rows");
	const enter = new w.Event("dragenter", { bubbles: true, cancelable: true });
	enter.dataTransfer = { types: ["Files"] };
	container.dispatchEvent(enter);
	assert.ok(container.classList.contains("drop-target"));
	const leave = new w.Event("dragleave", { bubbles: true });
	container.dispatchEvent(leave);
	assert.ok(!container.classList.contains("drop-target"));
	// without Files, no target mark
	const plain = new w.Event("dragenter", { bubbles: true });
	plain.dataTransfer = { types: ["text/plain"] };
	container.dispatchEvent(plain);
	assert.ok(!container.classList.contains("drop-target"));
});

test("the mode checkboxes and the voting type disable each other", async (t) => {
	const w = await page({ rows: [{ text: "A" }] });
	const sc = w.document.getElementById("single-choice");
	const ma = w.document.getElementById("must-answer");
	const ytYes = w.document.getElementById("votetype-yes");
	const ytYesNo = w.document.getElementById("votetype-yes-no");
	const ytYmn = w.document.getElementById("votetype-yes-maybe-no");
	const ytFree = w.document.getElementById("votetype-freetext");
	const scNote = w.document.getElementById("single-choice-note");
	const maNote = w.document.getElementById("must-answer-note");
	// the server-rendered default: yes-no checked, everything enabled
	assert.ok(ytYesNo.checked);
	assert.ok(sc.disabled === false);

	sc.checked = true;
	sc.dispatchEvent(new w.Event("change", { bubbles: true }));
	assert.ok(!ytYes.disabled && ytYes.checked); // the disabled yes-no falls out to the default
	assert.ok(ytYesNo.disabled && ytYmn.disabled);
	assert.ok(ma.disabled);
	assert.ok(!scNote.hidden);
	assert.ok(maNote.hidden);

	sc.checked = false;
	sc.dispatchEvent(new w.Event("change", { bubbles: true }));
	assert.ok(!ytYesNo.disabled && !ytYmn.disabled && !ma.disabled);
	assert.ok(ytYes.checked); // the fallback selection stays

	ma.checked = true;
	ma.dispatchEvent(new w.Event("change", { bubbles: true }));
	assert.ok(ytYes.disabled);
	assert.ok(ytYesNo.checked); // yes fell out to the yes-no default
	assert.ok(sc.disabled);

	// freetext is valid in every mode: it never falls out
	ytFree.checked = true;
	ytFree.dispatchEvent(new w.Event("change", { bubbles: true }));
	ma.checked = false;
	ma.dispatchEvent(new w.Event("change", { bubbles: true }));
	sc.checked = true;
	sc.dispatchEvent(new w.Event("change", { bubbles: true }));
	assert.ok(ytFree.checked);
});

test("the character counters mirror the caps and count the prefill", async (t) => {
	const w = await page({ rows: [{ text: "" }] });
	w.document.getElementById("title").value = "Pi";
	w.document.getElementById("title").dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.equal(w.document.getElementById("title-count").textContent, "2 / 100");
	w.document.getElementById("description").value = "abcdef";
	w.document.getElementById("description").dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.equal(w.document.getElementById("description-count").textContent, "6 / 5000");
});

test("clicking the disabled submit flashes the option hint", async (t) => {
	const w = await page({ rows: [{ text: "" }] });
	assert.ok(submit(w).disabled);
	submit(w).dispatchEvent(new w.MouseEvent("click", { bubbles: true, clientX: 0, clientY: 0 }));
	assert.ok(hint(w).classList.contains("err"));
	hint(w).dispatchEvent(new w.Event("animationend"));
	assert.ok(!hint(w).classList.contains("err"));
});
