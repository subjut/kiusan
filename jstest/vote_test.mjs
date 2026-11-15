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
