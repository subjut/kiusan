// poll.js — the meeting page's answers form row: the green / yellow mark
// toggles, the hidden state inputs, and the single-slot grey-out.
import test from "node:test";
import assert from "node:assert/strict";
import { boot, settled, click } from "./harness.mjs";

const S1 = "2026-11-01T10:00Z";
const S2 = "2026-11-02T10:00Z";
const S3 = "2026-11-03T10:00Z";

function formCell(slot, { status = "", yellow = true } = {}) {
	return `<td class="cell form-cell" data-slot="${slot}" data-status="${status}">
	<input type="hidden" name="a${slot}" value="${status}">
	<div class="mark-stack">
	  <button type="button" class="mark mark-g" aria-pressed="false">✓</button>
	  ${yellow ? '<button type="button" class="mark mark-y" aria-pressed="false">(✓)</button>' : ""}
	</div>
  </td>`;
}

// The table's submitted-answers row stays untouched by the wiring — the
// fixture carries one so the tests can assert that.
function fixture({ single = false, prefill = [] } = {}) {
	const cells = [S1, S2, S3].map((s, i) =>
		formCell(s, { status: prefill[i] || "", yellow: !single }));
	return `<form id="answer-form" data-single="${single}">
	<div class="table-breakout"><div class="table-scroll"><table class="answers-table">
	<tbody>
	<tr><td>alice</td><td class="cell st-g">✓</td><td class="cell">✗</td><td class="cell st-y">(✓)</td></tr>
	<tr class="form-row">
	  <td><input type="text" id="name" name="name" maxlength="50" required></td>
	  ${cells.join("\n")}
	</tr>
	</tbody>
	</table></div></div>
  </form>`;
}

async function page(opts = {}) {
	const w = boot(fixture(opts), { scripts: ["page.js", "poll.js"] });
	await settled(w);
	return w;
}

const cells = (w) => [...w.document.querySelectorAll("td.form-cell")];

test("a green mark toggles on and off", async (t) => {
	const w = await page();
	const c = cells(w)[0];
	click(w, c.querySelector(".mark-g"));
	assert.equal(c.getAttribute("data-status"), "g");
	assert.ok(c.classList.contains("st-g"));
	assert.equal(c.querySelector("input").value, "g");
	assert.equal(c.querySelector(".mark-g").getAttribute("aria-pressed"), "true");
	click(w, c.querySelector(".mark-g"));
	assert.equal(c.getAttribute("data-status"), "");
	assert.equal(c.querySelector("input").value, "");
	assert.equal(c.querySelector(".mark-g").getAttribute("aria-pressed"), "false");
	// an unmarked form cell stays neutral — no red tint
	assert.ok(!c.classList.contains("st-g"));
	assert.ok(!c.classList.contains("st-r"));
});

test("yellow toggles, and a mark switches between the two", async (t) => {
	const w = await page();
	const c = cells(w)[1];
	click(w, c.querySelector(".mark-y"));
	assert.equal(c.getAttribute("data-status"), "y");
	assert.ok(c.classList.contains("st-y"));
	assert.equal(c.querySelector(".mark-y").getAttribute("aria-pressed"), "true");
	// green replaces yellow (one state per cell)
	click(w, c.querySelector(".mark-g"));
	assert.equal(c.getAttribute("data-status"), "g");
	assert.ok(c.classList.contains("st-g"));
	assert.ok(!c.classList.contains("st-y"));
	assert.equal(c.querySelector(".mark-y").getAttribute("aria-pressed"), "false");
});

test("submitted answer rows are left alone", async (t) => {
	const w = await page();
	const alice = w.document.querySelector("tbody tr");
	const tds = [...alice.querySelectorAll("td")];
	click(w, cells(w)[0].querySelector(".mark-g"));
	assert.deepEqual(tds.slice(1).map((td) => td.className),
		["cell st-g", "cell", "cell st-y"]);
});

test("a multi-slot poll keeps every cell clickable", async (t) => {
	const w = await page();
	const all = cells(w);
	click(w, all[1].querySelector(".mark-g"));
	assert.ok(!all[0].classList.contains("greyed"));
	assert.ok(!all[2].classList.contains("greyed"));
	for (const c of all)
		for (const b of c.querySelectorAll("button.mark"))
			assert.ok(!b.disabled);
});

test("a single-slot poll greys out the other slots", async (t) => {
	const w = await page({ single: true });
	const all = cells(w);
	click(w, all[1].querySelector(".mark-g"));
	assert.equal(all[1].getAttribute("data-status"), "g");
	assert.ok(!all[1].classList.contains("greyed"));
	for (const c of [all[0], all[2]]) {
		assert.ok(c.classList.contains("greyed"));
		for (const b of c.querySelectorAll("button.mark"))
			assert.ok(b.disabled);
	}
	// unmarking re-enables the rest
	click(w, all[1].querySelector(".mark-g"));
	assert.equal(all[1].getAttribute("data-status"), "");
	for (const c of all) {
		assert.ok(!c.classList.contains("greyed"));
		for (const b of c.querySelectorAll("button.mark"))
			assert.ok(!b.disabled);
	}
});

test("a greyed slot cannot be marked", async (t) => {
	const w = await page({ single: true });
	const all = cells(w);
	click(w, all[0].querySelector(".mark-g"));
	// the browser would not deliver a click to a disabled button at all;
	// the handler's own guard must drop it either way
	click(w, all[1].querySelector(".mark-g"));
	assert.equal(all[1].getAttribute("data-status"), "");
	assert.equal(all[0].getAttribute("data-status"), "g");
});

test("an edited row is prefilled and editable", async (t) => {
	const w = await page({ prefill: ["g"] });
	const c = cells(w)[0];
	assert.equal(c.getAttribute("data-status"), "g");
	assert.ok(c.classList.contains("st-g"));
	assert.equal(c.querySelector("input").value, "g");
	assert.equal(c.querySelector(".mark-g").getAttribute("aria-pressed"), "true");
	click(w, c.querySelector(".mark-y"));
	assert.equal(c.getAttribute("data-status"), "y");
	assert.equal(c.querySelector("input").value, "y");
});
