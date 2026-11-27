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
// every option carries a selection, and the instruction below the form
// reads amber, green once the answer is complete (amber again when an
// option is deselected), and red with a short shake after a click on the
// greyed button (scrolled into view first). The server re-checks every
// constraint on submit. Trash icons fill and submit the page's hidden delete form.

(function () {
	"use strict";

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
			var note = main.querySelector("p.must-hint");
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
				if (note) {
					// Green while the answer is complete, amber again as soon
					// as an option is deselected; the click flash (.err) is
					// dropped once the answer is complete.
					if (complete()) {
						note.classList.remove("err");
						note.classList.add("ok");
					} else {
						note.classList.remove("ok");
					}
				}
			};

			// A disabled button swallows its own click events, so a click on
			// it lands on an ancestor — hit-test the coordinates against the
			// button's rect to catch it.
			var flashTimer = null;
			var flashNote = function () {
				if (!note) return;
				if (flashTimer) {
					window.clearTimeout(flashTimer);
					flashTimer = null;
				}
				var shake = function () {
					// Remove before re-adding (reflow between) so the shake
					// restarts on a repeated click.
					note.classList.remove("err");
					void note.offsetWidth;
					note.classList.add("err");
				};
				var r = note.getBoundingClientRect();
				if (r.top >= 0 && r.bottom <= window.innerHeight) {
					shake();
				} else {
					note.scrollIntoView({ block: "center", behavior: "smooth" });
					flashTimer = window.setTimeout(function () {
						flashTimer = null;
						shake();
					}, 250);
				}
			};
			if (note) {
				note.addEventListener("animationend", function () {
					note.classList.remove("err");
				});
			}
			main.addEventListener("click", function (e) {
				refreshSubmit();
				if (!submitBtn || !note || !submitBtn.disabled) return;
				var b = submitBtn.getBoundingClientRect();
				if (
					e.clientX >= b.left &&
					e.clientX <= b.right &&
					e.clientY >= b.top &&
					e.clientY <= b.bottom
				) {
					flashNote();
				}
			});
			main.addEventListener("input", refreshSubmit);
			refreshSubmit();
		}
	}

	// ---- the lightbox ------------------------------------------------------
	// The image options' thumbnails in the table header: a click opens
	// the full size over the greyed backdrop (the .lightbox in the page
	// template). Esc, the backdrop, and the x close it (focus returns to
	// the thumbnail); the prev/next arrows and the left/right keys cycle
	// through the image options. The controls are positioned around the
	// largest image of the cycle — the close button just above + to the
	// right of its top-right corner, the arrows just outside its widest
	// edges — so none of them ever overlays an image, and the arrows hold
	// their place while cycling (a moving button is a moving target).
	var lb = document.getElementById("lightbox");
	if (lb) {
		var lbImg = lb.querySelector(".lightbox-img");
		var lbClose = lb.querySelector(".lightbox-close");
		var lbPrev = lb.querySelector(".lightbox-prev");
		var lbNext = lb.querySelector(".lightbox-next");
		var thumbs = Array.prototype.slice.call(
			document.querySelectorAll("th .choice-thumb")
		);
		var openIdx = -1;
		var opener = null;
		// The full images' true sizes, learned as each one loads: the
		// stored images are ≤ 1024px while the display box is 96vw × 96vh,
		// and max-width/max-height never upscale — on a normal viewport
		// an image renders at its natural size, which the thumbnails'
		// aspect ratios can't predict. Anchoring on the box-fitted size
		// is what sent the controls hundreds of px away from the image.
		var fullSizes = new Array(thumbs.length);

		// The display size of one image in the 96vw × 96vh box (contain,
		// scale capped at 1 — the image is never upscaled): the true
		// size while known, the thumbnail's aspect (exact while it
		// downscale-fits the box) before.
		function dispSize(i, boxW, boxH) {
			var d = fullSizes[i];
			var img = thumbs[i].querySelector("img");
			var nw = (d && d.w) || (img && img.naturalWidth) || 1;
			var nh = (d && d.h) || (img && img.naturalHeight) || 1;
			var s = Math.min(1, boxW / nw, boxH / nh);
			return { w: nw * s, h: nh * s };
		}

		// The widest / tallest display size of the cycle — the union box
		// the controls anchor on (every image fits inside it, so the
		// controls sit clear of all of them).
		function cycleBox() {
			var boxW = 0.96 * window.innerWidth;
			var boxH = 0.96 * window.innerHeight;
			var maxW = 0;
			var maxH = 0;
			thumbs.forEach(function (_, i) {
				var d = dispSize(i, boxW, boxH);
				maxW = Math.max(maxW, d.w);
				maxH = Math.max(maxH, d.h);
			});
			return { w: maxW, h: maxH };
		}

		// The controls' 44px squares + 4px off the image (the metrics of
		// .lightbox-close / -prev / -next in style.css).
		var BTN = 44;
		var GAP = 4;

		function placeControls() {
			var b = cycleBox();
			var cx = window.innerWidth / 2;
			var cy = window.innerHeight / 2;
			if (lbClose) {
				lbClose.style.left =
					Math.min(
						cx + b.w / 2 + GAP,
						window.innerWidth - BTN - 8
					) + "px";
				lbClose.style.top =
					Math.max(cy - b.h / 2 - BTN - GAP, 8) + "px";
			}
			var top = cy - BTN / 2;
			if (lbPrev) {
				lbPrev.style.left =
					Math.max(8, cx - b.w / 2 - BTN - GAP) + "px";
				lbPrev.style.top = top + "px";
			}
			if (lbNext) {
				lbNext.style.left =
					Math.min(
						cx + b.w / 2 + GAP,
						window.innerWidth - BTN - 8
					) + "px";
				lbNext.style.top = top + "px";
			}
		}

		function learn(i, w, h) {
			if (i < 0 || !w || !h) return;
			fullSizes[i] = { w: w, h: h };
			if (!lb.hidden) placeControls();
		}

		function show(i) {
			if (i < 0 || i >= thumbs.length) return;
			openIdx = i;
			var thumb = thumbs[i];
			if (lbImg) {
				lbImg.src = thumb.getAttribute("data-src");
				var img = thumb.querySelector("img");
				lbImg.alt = (img && img.alt) || "";
				// a cached image reports its size right away; the rest
				// lands with the load event
				learn(i, lbImg.naturalWidth, lbImg.naturalHeight);
			}
			lb.hidden = false;
			placeControls();
			if (lbClose) lbClose.focus();
		}

		function close() {
			lb.hidden = true;
			openIdx = -1;
			if (opener) {
				opener.focus();
				opener = null;
			}
		}

		function cycle(d) {
			if (thumbs.length < 2) return;
			show((openIdx + d + thumbs.length) % thumbs.length);
		}

		// A single-image vote has nothing to cycle to.
		if (thumbs.length < 2) {
			if (lbPrev) lbPrev.hidden = true;
			if (lbNext) lbNext.hidden = true;
		}

		thumbs.forEach(function (thumb, i) {
			thumb.addEventListener("click", function () {
				opener = thumb;
				show(i);
			});
		});
		if (lbClose) lbClose.addEventListener("click", close);
		if (lbPrev)
			lbPrev.addEventListener("click", function () {
				cycle(-1);
			});
		if (lbNext)
			lbNext.addEventListener("click", function () {
				cycle(1);
			});
		// Learn the full sizes as they load: the main image's load event
		// (a cached one reports right away in show) plus a small up-front
		// preload of the whole cycle, so the anchor is exact from the
		// first open; each arrival re-anchors while the lightbox is open.
		if (lbImg) {
			lbImg.addEventListener("load", function () {
				learn(openIdx, lbImg.naturalWidth, lbImg.naturalHeight);
			});
		}
		thumbs.forEach(function (thumb, i) {
			var pre = new Image();
			pre.onload = function () {
				learn(i, pre.naturalWidth, pre.naturalHeight);
			};
			pre.src = thumb.getAttribute("data-src");
		});
		lb.addEventListener("click", function (e) {
			if (e.target === lb) close();
		});
		window.addEventListener("resize", function () {
			if (!lb.hidden) placeControls();
		});
		document.addEventListener("keydown", function (e) {
			if (lb.hidden) return;
			if (e.key === "Escape") {
				close();
			} else if (e.key === "ArrowRight") {
				cycle(1);
			} else if (e.key === "ArrowLeft") {
				cycle(-1);
			}
		});
	}

})();
