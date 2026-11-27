// vote.js — the vote page's answers form row: the mark toggles per
// voting type, the single-choice exclusivity, the free-text note gate,
// and the must-answer submit gate.
import test from "node:test";
import assert from "node:assert/strict";
import { boot, settled, click } from "./harness.mjs";

const SYM = { g: "✓", y: "(✓)", r: "✗" };
const MARKS = { yes: ["g"], "yes-no": ["g", "r"], "yes-maybe-no": ["g", "y", "r"] };

function formCell(id, type, status = "") {
	if (type === "freetext") {
		return `<td class="cell form-cell" data-choice="${id}">
	<input type="text" name="n${id}" maxlength="150">
  </td>`;
	}
	const btns = MARKS[type].map((m) =>
		`<button type="button" class="mark mark-${m}" aria-pressed="false">${SYM[m]}</button>`).join("");
	return `<td class="cell form-cell" data-choice="${id}" data-status="${status}">
	<input type="hidden" name="a${id}" value="${status}">
	<div class="mark-stack">${btns}</div>
  </td>`;
}

function fixture({ type = "yes-no", single = false, must = false, choices = ["c1", "c2", "c3"] } = {}) {
	const hint = must ? '<p class="form-note must-hint">Answer every option</p>' : "";
	return `<form id="answer-form" data-type="${type}" data-single="${single}" data-must="${must}">
	<div class="table-breakout"><div class="table-scroll"><table class="answers-table vote-table">
	<tbody>
	<tr class="form-row">
	  <td><input type="text" id="name" name="name" maxlength="50" required></td>
	  ${choices.map((c) => formCell(c, type)).join("\n")}
	</tr>
	</tbody>
	</table></div></div>
	${hint}
	<div class="form-actions"><button type="submit">Save</button></div>
  </form>`;
}

async function page(opts = {}) {
	const w = boot(fixture(opts), { scripts: ["page.js", "vote.js"] });
	await settled(w);
	return w;
}

const cells = (w) => [...w.document.querySelectorAll("td.form-cell")];
const status = (c) => c.getAttribute("data-status");
const inputOf = (c) => c.querySelector("input");

test("yes / no toggles paint, unmark, and replace each other", async (t) => {
	const w = await page({ type: "yes-no" });
	const c = cells(w)[0];
	click(w, c.querySelector(".mark-g"));
	assert.equal(status(c), "y");
	assert.ok(c.classList.contains("st-g"));
	assert.equal(inputOf(c).value, "y");
	assert.equal(c.querySelector(".mark-g").getAttribute("aria-pressed"), "true");
	click(w, c.querySelector(".mark-r")); // no replaces yes (one state per cell)
	assert.equal(status(c), "n");
	assert.ok(c.classList.contains("st-r"));
	assert.ok(!c.classList.contains("st-g"));
	assert.equal(inputOf(c).value, "n");
	assert.equal(c.querySelector(".mark-r").getAttribute("aria-pressed"), "true");
	assert.equal(c.querySelector(".mark-g").getAttribute("aria-pressed"), "false");
	click(w, c.querySelector(".mark-r"));
	assert.equal(status(c), "");
	assert.equal(inputOf(c).value, "");
	assert.ok(!c.classList.contains("st-r"));
});

test("a yes vote has no no-button and stays neutral when untouched", async (t) => {
	const w = await page({ type: "yes" });
	const c = cells(w)[0];
	assert.ok(c.querySelector(".mark-g"));
	assert.ok(!c.querySelector(".mark-y"));
	assert.ok(!c.querySelector(".mark-r"));
	assert.ok(!c.classList.contains("st-r")); // no implicit no
	click(w, c.querySelector(".mark-g"));
	assert.equal(status(c), "y");
	click(w, c.querySelector(".mark-g"));
	assert.equal(status(c), "");
});

test("yes / maybe / no adds the maybe toggle", async (t) => {
	const w = await page({ type: "yes-maybe-no" });
	const c = cells(w)[1];
	click(w, c.querySelector(".mark-y"));
	assert.equal(status(c), "m");
	assert.ok(c.classList.contains("st-y"));
	assert.equal(c.querySelector(".mark-y").getAttribute("aria-pressed"), "true");
	assert.equal(inputOf(c).value, "m");
	click(w, c.querySelector(".mark-y"));
	assert.equal(status(c), "");
});

test("single choice keeps exactly one yes", async (t) => {
	const w = await page({ type: "yes-no", single: true });
	const [a, b, c] = cells(w);
	click(w, a.querySelector(".mark-g"));
	click(w, b.querySelector(".mark-g"));
	assert.equal(status(b), "y");
	assert.equal(status(a), ""); // the previous yes went back to neutral
	assert.equal(status(c), "");
	assert.equal(inputOf(a).value, "");
	assert.equal(inputOf(b).value, "y");
	// the new selection is not undone by re-clicking it
	click(w, b.querySelector(".mark-g"));
	assert.equal(status(b), "");
	assert.equal(status(a), "");
});

test("a single-choice free text allows exactly one note", async (t) => {
	const w = await page({ type: "freetext", single: true });
	const [a, b, c] = cells(w);
	const inp = (x) => x.querySelector("input");
	inp(a).value = "Pizza";
	inp(a).dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.equal(inp(b).value, "");
	assert.equal(inp(c).value, "");
	assert.ok(inp(b).disabled);
	assert.ok(inp(c).disabled);
	assert.ok(!inp(a).disabled);
	// emptying the note re-enables the rest
	inp(a).value = "";
	inp(a).dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.ok(!inp(b).disabled);
	assert.ok(!inp(c).disabled);
});

test("a non-single free text allows every note", async (t) => {
	const w = await page({ type: "freetext" });
	const [a, b] = cells(w);
	const inp = (x) => x.querySelector("input");
	inp(a).value = "x";
	inp(a).dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.ok(!inp(b).disabled);
	assert.equal(inp(b).value, "");
});

test("must answer keeps the submit disabled until every option is marked", async (t) => {
	const w = await page({ type: "yes-no", must: true });
	const submit = w.document.querySelector('button[type="submit"]');
	const hint = w.document.querySelector(".must-hint");
	const [a, b, c] = cells(w);
	assert.ok(submit.disabled);
	assert.ok(!hint.classList.contains("ok"));
	click(w, a.querySelector(".mark-g"));
	assert.ok(submit.disabled); // one option left
	click(w, b.querySelector(".mark-r"));
	click(w, c.querySelector(".mark-g"));
	assert.ok(!submit.disabled);
	assert.ok(hint.classList.contains("ok"));
	// deselecting one option re-locks and drops the green
	click(w, a.querySelector(".mark-g"));
	assert.ok(submit.disabled);
	assert.ok(!hint.classList.contains("ok"));
});

test("must answer on a free text votes on trimmed notes", async (t) => {
	const w = await page({ type: "freetext", must: true });
	const submit = w.document.querySelector('button[type="submit"]');
	const [a, b, c] = cells(w);
	const inp = (x) => x.querySelector("input");
	assert.ok(submit.disabled);
	inp(a).value = "a";
	inp(b).value = "b";
	inp(c).value = "c";
	for (const x of [a, b, c])
		inp(x).dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.ok(!submit.disabled);
	inp(b).value = "   "; // whitespace is not an answer
	inp(b).dispatchEvent(new w.Event("input", { bubbles: true }));
	assert.ok(submit.disabled);
});

test("clicking the disabled submit flashes the hint", async (t) => {
	const w = await page({ type: "yes-no", must: true });
	const submit = w.document.querySelector('button[type="submit"]');
	const hint = w.document.querySelector(".must-hint");
	// a disabled button swallows real clicks; the handler hit-tests the
	// coordinates against the button's rect (zero-sized in jsdom, so
	// 0,0 lands inside it)
	submit.dispatchEvent(new w.MouseEvent("click",
		{ bubbles: true, clientX: 0, clientY: 0 }));
	assert.ok(hint.classList.contains("err"));
	hint.dispatchEvent(new w.Event("animationend"));
	assert.ok(!hint.classList.contains("err"));
});

test("without must answer the submit is never gated", async (t) => {
	const w = await page({ type: "yes-no" });
	assert.ok(!w.document.querySelector('button[type="submit"]').disabled);
	assert.ok(!w.document.querySelector(".must-hint"));
});

// ---- the lightbox ----
// The image options' thumbnails in the table header open the full size
// over the greyed backdrop: Esc, the backdrop, the x, close it (focus
// returns to the thumbnail); the prev/next arrows and the left/right
// keys cycle through the image options. The controls are positioned
// around the largest image of the cycle, so they never overlay one and
// the arrows hold their place while cycling.

function lightboxFixture(opts = {}) {
	const thumb = (id, label) => `<th scope="col" class="th-choice th-img"><span class="choice-head">
${label ? `<span class="choice-head-text">${label}</span>` : ""}
<button type="button" class="choice-thumb" data-src="/vote-img/${id}">
<img src="/vote-img/${id}/thumb" alt="${label || "Option"}" width="64" height="64">
</button>
</span></th>`;
	const second = opts.single ? "" : thumb("img-bbbb2222", "");
	return `<form id="answer-form" data-type="yes-no" data-single="false" data-must="false">
<table class="answers-table vote-table">
<thead><tr>
<th scope="col" class="th-name">Name</th>
${thumb("img-aaaa1111", "Pic")}
<th scope="col" class="th-choice">Plain</th>
${second}
</tr></thead>
<tbody><tr class="form-row"><td><input type="text" id="name" name="name"></td>
<td class="cell form-cell" data-choice="c1" data-status=""><input type="hidden" name="ac1" value=""><div class="mark-stack"><button type="button" class="mark mark-g" aria-pressed="false">✓</button></div></td>
<td class="cell form-cell" data-choice="c2" data-status=""><input type="hidden" name="ac2" value=""><div class="mark-stack"><button type="button" class="mark mark-g" aria-pressed="false">✓</button></div></td>
${opts.single ? "" : '<td class="cell form-cell" data-choice="c3" data-status=""><input type="hidden" name="ac3" value=""><div class="mark-stack"><button type="button" class="mark mark-g" aria-pressed="false">✓</button></div></td>'}
</tr></tbody>
</table>
</form>
<div class="lightbox" id="lightbox" hidden role="dialog" aria-modal="true" aria-label="Option image">
<img class="lightbox-img" alt="">
<button type="button" class="lightbox-prev" aria-label="Previous image">‹</button>
<button type="button" class="lightbox-next" aria-label="Next image">›</button>
<button type="button" class="lightbox-close" aria-label="Close image">×</button>
</div>`;
}

async function lightboxPage() {
	const w = boot(lightboxFixture(), { scripts: ["page.js", "vote.js"] });
	await settled(w);
	return w;
}

const lb = (w) => w.document.getElementById("lightbox");
const lbImg = (w) => lb(w).querySelector(".lightbox-img");

const keydown = (w, k) =>
w.document.dispatchEvent(new w.KeyboardEvent("keydown", { key: k, bubbles: true }));

test("clicking a thumbnail opens the full size over the backdrop", async (t) => {
	const w = await lightboxPage();
	const [t1, t2] = [...w.document.querySelectorAll(".choice-thumb")];
	assert.ok(lb(w).hidden);
	click(w, t1);
	assert.ok(!lb(w).hidden);
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-aaaa1111");
	assert.equal(lbImg(w).getAttribute("alt"), "Pic");
	assert.equal(w.document.activeElement, lb(w).querySelector(".lightbox-close"));
	// a text option has no thumbnail
	assert.equal(w.document.querySelectorAll(".choice-thumb").length, 2);
	click(w, t2);
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-bbbb2222");
	assert.equal(lbImg(w).getAttribute("alt"), "Option");
});

test("the close button, the backdrop, and Escape all close (focus returns)", async (t) => {
	const w = await lightboxPage();
	const t1 = w.document.querySelector(".choice-thumb");
	click(w, t1);
	assert.ok(!lb(w).hidden);
	// the x closes and hands focus back to the opener
	click(w, lb(w).querySelector(".lightbox-close"));
	assert.ok(lb(w).hidden);
	assert.equal(w.document.activeElement, t1);

	// the backdrop: only a click on the box itself closes it
	click(w, t1);
	assert.ok(!lb(w).hidden);
	click(w, lbImg(w)); // a click on the image is not a backdrop click
	assert.ok(!lb(w).hidden);
	click(w, lb(w));
	assert.ok(lb(w).hidden);

	// Escape
	click(w, t1);
	assert.ok(!lb(w).hidden);
	keydown(w, "Escape");
	assert.ok(lb(w).hidden);
	// a keydown while closed is a no-op
	keydown(w, "ArrowRight");
	assert.ok(lb(w).hidden);
});

test("the arrows cycle through the image options and wrap", async (t) => {
	const w = await lightboxPage();
	const t1 = w.document.querySelector(".choice-thumb");
	click(w, t1);
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-aaaa1111");
	keydown(w, "ArrowRight");
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-bbbb2222");
	keydown(w, "ArrowRight"); // wraps around
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-aaaa1111");
	keydown(w, "ArrowLeft"); // wraps the other way
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-bbbb2222");
});

test("the prev/next buttons cycle through the image options and wrap", async (t) => {
	const w = await lightboxPage();
	const t1 = w.document.querySelector(".choice-thumb");
	click(w, t1);
	const box = lb(w);
	const prev = box.querySelector(".lightbox-prev");
	const next = box.querySelector(".lightbox-next");
	assert.ok(!prev.hidden);
	assert.ok(!next.hidden);
	click(w, next);
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-bbbb2222");
	click(w, next); // wraps around
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-aaaa1111");
	click(w, prev); // wraps the other way
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-bbbb2222");
});

// jsdom never loads images, so the intrinsic sizes are pinned by hand
// (an own property shadows the prototype getter).
function intrinsic(w, width, height) {
	return function (img) {
		Object.defineProperty(img, "naturalWidth", { value: width });
		Object.defineProperty(img, "naturalHeight", { value: height });
	};
}

test("the controls sit around the largest image of the cycle", async (t) => {
	const w = await lightboxPage();
	const [i1, i2] = [...w.document.querySelectorAll(".choice-thumb img")];
	intrinsic(w, 1024, 512)(i1); // the wide one
	intrinsic(w, 512, 1024)(i2); // the tall one
	const t1 = w.document.querySelector(".choice-thumb");
	click(w, t1);
	const close = lb(w).querySelector(".lightbox-close");
	const prev = lb(w).querySelector(".lightbox-prev");
	const next = lb(w).querySelector(".lightbox-next");
	// jsdom's viewport is 1024 x 768, so the 96vw x 96vh box is
	// 983.04 x 737.28: the wide image fits 983.04 x 491.52, the tall
	// one 368.64 x 737.28 — the union is 983.04 x 737.28. The close
	// button sits above + right of its top-right corner (clamped to the
	// viewport's 8px margin), the arrows just outside the widest
	// image's edges, vertically centered.
	assert.ok(Math.abs(parseFloat(close.style.left) - 972) < 0.01);
	assert.ok(Math.abs(parseFloat(close.style.top) - 8) < 0.01);
	assert.ok(Math.abs(parseFloat(prev.style.left) - 8) < 0.01);
	assert.ok(Math.abs(parseFloat(next.style.left) - 972) < 0.01);
	assert.ok(Math.abs(parseFloat(prev.style.top) - 362) < 0.01);
	assert.ok(Math.abs(parseFloat(next.style.top) - 362) < 0.01);
	// cycling keeps the controls in the same place
	keydown(w, "ArrowRight");
	assert.ok(Math.abs(parseFloat(prev.style.left) - 8) < 0.01);
	assert.ok(Math.abs(parseFloat(next.style.left) - 972) < 0.01);
	assert.ok(Math.abs(parseFloat(next.style.top) - 362) < 0.01);
});

test("images smaller than the box are not upscaled — the controls hug them", async (t) => {
	const w = await lightboxPage();
	const [i1, i2] = [...w.document.querySelectorAll(".choice-thumb img")];
	intrinsic(w, 500, 500)(i1); // natural-size photos below the 1024 cap
	intrinsic(w, 400, 400)(i2);
	click(w, w.document.querySelector(".choice-thumb"));
	const close = lb(w).querySelector(".lightbox-close");
	const prev = lb(w).querySelector(".lightbox-prev");
	const next = lb(w).querySelector(".lightbox-next");
	// the 96vw x 96vh box (983.04 x 737.28) exceeds both images —
	// max-width/max-height never upscale, so the anchor is the natural
	// 500 x 500, not the box-fitted 735 x 735 the old math assumed
	assert.ok(Math.abs(parseFloat(close.style.left) - 766) < 0.01);
	assert.ok(Math.abs(parseFloat(close.style.top) - 86) < 0.01);
	assert.ok(Math.abs(parseFloat(prev.style.left) - 214) < 0.01);
	assert.ok(Math.abs(parseFloat(next.style.left) - 766) < 0.01);
	assert.ok(Math.abs(parseFloat(next.style.top) - 362) < 0.01);
});

test("a single-image vote hides the prev/next arrows", async (t) => {
	const w = boot(lightboxFixture({ single: true }), {
		scripts: ["page.js", "vote.js"],
	});
	await settled(w);
	const t1 = w.document.querySelector(".choice-thumb");
	click(w, t1);
	assert.ok(!lb(w).hidden);
	assert.ok(lb(w).querySelector(".lightbox-prev").hidden);
	assert.ok(lb(w).querySelector(".lightbox-next").hidden);
	// keyboard cycling stays a no-op, Escape closes
	keydown(w, "ArrowRight");
	assert.equal(lbImg(w).getAttribute("src"), "/vote-img/img-aaaa1111");
	keydown(w, "Escape");
	assert.ok(lb(w).hidden);
});
