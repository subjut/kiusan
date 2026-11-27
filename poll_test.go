package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// pollClient drives the app in-process, the way a respondent's or the
// organizer's browser would: one raw identity cookie per request, and
// redirects are asserted on, not followed.
type pollClient struct {
	t   *testing.T
	app *fiber.App
}

func newPollClient(t *testing.T, app *fiber.App) *pollClient {
	t.Helper()
	return &pollClient{t: t, app: app}
}

// do runs one request. form is the url-encoded body of a POST ("" for
// GETs), cookie a raw name=value pair ("" for none), contentType overrides
// the body content type (default: urlencoded — the only one the site's
// forms produce). A POST always carries the urlencoded content type, even
// with an empty body — as a field-less browser form submission does.
func (p *pollClient) do(method, path, form, cookie, contentType string) *http.Response {
	p.t.Helper()
	ct := contentType
	if ct == "" {
		ct = "application/x-www-form-urlencoded"
	}
	var req *http.Request
	if form != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(form))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	if form != "" || method == http.MethodPost {
		req.Header.Set("Content-Type", ct)
	}
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	r, err := p.app.Test(req)
	if err != nil {
		p.t.Fatalf("%s %s: %v", method, path, err)
	}
	return r
}

func (p *pollClient) get(path, cookie string) *http.Response {
	p.t.Helper()
	return p.do(http.MethodGet, path, "", cookie, "")
}

func (p *pollClient) post(path, form, cookie string) *http.Response {
	p.t.Helper()
	return p.do(http.MethodPost, path, form, cookie, "")
}

func (p *pollClient) body(r *http.Response) string {
	p.t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		p.t.Fatalf("reading body: %v", err)
	}
	return string(b)
}

// cookie returns the value the response sets for the named cookie.
func (p *pollClient) cookie(r *http.Response, name string) string {
	p.t.Helper()
	for _, sc := range r.Header.Values("Set-Cookie") {
		if rest, ok := strings.CutPrefix(sc, name+"="); ok {
			if i := strings.IndexByte(rest, ';'); i >= 0 {
				return rest[:i]
			}
			return rest
		}
	}
	p.t.Fatalf("no Set-Cookie for %q in %v", name, r.Header.Values("Set-Cookie"))
	return ""
}

// hasCookie reports whether the response sets the named cookie at all.
func (p *pollClient) hasCookie(r *http.Response, name string) bool {
	for _, sc := range r.Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, name+"=") {
			return true
		}
	}
	return false
}

// createPoll submits the 1-1 form and returns the stored meeting plus the
// admin link key from the 303's Location (creation lands on the admin
// view, where both links live).
func (p *pollClient) createPoll(form string) (*database.Meeting, string) {
	p.t.Helper()
	r := p.post("/meet-form", form, "")
	if r.StatusCode != http.StatusSeeOther {
		p.t.Fatalf("POST /meet-form: expected 303, got %d", r.StatusCode)
	}
	loc := r.Header.Get("Location")
	adminKey := strings.TrimPrefix(loc, "/meet/")
	if adminKey == "" || adminKey == loc {
		p.t.Fatalf("POST /meet-form: unexpected Location %q", loc)
	}
	m, err := db.GetAdmin(adminKey)
	if err != nil {
		p.t.Fatalf("looking up %q through the store: %v", adminKey, err)
	}
	return m, adminKey
}

// runPollSuite exercises the two-link poll model end to end: create, both
// views, the per-poll identity cookie, CSRF-gated answering, answer
// editing and deletion, admin management, poll editing (keys must survive),
// expiry and prolongation, and the clickjacking headers. One sequential
// walkthrough, as the old suites are shaped; it runs on the sqlite backend.
func runPollSuite(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	p := newPollClient(t, newTestApp(t))

	// Slot keys exactly as the handlers compute them: the slot's absolute
	// instant in UTC.
	s1 := slotKey(time.Date(2026, 11, 1, 10, 0, 0, 0, berlin))
	s2 := slotKey(time.Date(2026, 11, 1, 14, 0, 0, 0, berlin))

	// End dates are derived from now: parseEndDate rejects a past end date,
	// so a fixed calendar date would rot the suite once it has passed. The
	// candidate slots stay fixed — a slot may be in the past, an end date
	// may not.
	endA, expA := testEndDate(berlin)
	endAEd, expAEd := testEndDate(berlin)
	endD, _ := testEndDate(berlin)
	endDP, expDP := testEndDate(berlin)

	contains := func(b, want string) {
		p.t.Helper()
		if !strings.Contains(b, want) {
			p.t.Fatalf("body missing %q", want)
		}
	}
	absent := func(b, what string) {
		p.t.Helper()
		if strings.Contains(b, what) {
			p.t.Fatalf("body must not contain %q", what)
		}
	}
	status := func(r *http.Response, code int) {
		p.t.Helper()
		if r.StatusCode != code {
			p.t.Fatalf("expected %d, got %d", code, r.StatusCode)
		}
	}

	// ---- poll A: the full answer lifecycle ----
	mA, adminA := p.createPoll("title=Poll+A&email=poll%40example.com&description=Pick+a+time&timezone=Europe/Berlin&enddate=" + endA + "&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00")
	if mA.Key == "" || mA.AdminKey != adminA {
		t.Fatalf("poll A keys: user %q, admin %q (Location %q)", mA.Key, mA.AdminKey, adminA)
	}
	if got, err := db.Get(adminA); err == nil || got != nil {
		t.Fatalf("admin key must not resolve as a user key (err %v)", err)
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("admin key lookup: %v", err)
	}
	if !mA.Expires.Equal(expA) {
		t.Fatalf("poll A expiry: got %v, want %v", mA.Expires, expA)
	}
	cookie := cookieName(mA)

	// First visitor to the respondent page: a fresh identity cookie is
	// minted, the form and CSRF token render, and the page offers no poll
	// editing.
	r := p.get("/meet/"+mA.Key, "")
	status(r, http.StatusOK)
	alice := p.cookie(r, cookie)
	bUser := p.body(r)
	for _, want := range []string{
		"Poll A", "Pick a time",
		// The meta line carries the end date and the auto-deletion date
		// (finished + the pinned retention — while open: end + 6 months).
		`data-i18n="meet.endsLabel"`,
		`data-slot="` + slotKey(expA) + `" data-style="medium">`,
		`data-i18n="meet.deletesLabel"`,
		`data-slot="` + slotKey(mA.DeletionDate(cfg.FinishedTTL)) + `"`,
		`class="tz-name" data-base="Europe/Berlin">Europe/Berlin</span>`,
		`class="th-year" colspan="2"`, "2026",
		`class="th-date" colspan="2"`, `title="Sun 1 Nov 2026"`,
		"1 Nov\nSun",
		`class="th-time"`, "10:00", "14:00",
		`id="answer-form"`, `data-single="false"`,
		`name="csrftoken"`, `value="` + csrfToken(alice) + `"`,
	} {
		if !strings.Contains(bUser, want) {
			t.Fatalf("respondent page missing %q", want)
		}
	}
	bPre := p.body(p.get("/meet/"+mA.Key, cookie+"="+alice))
	absent(bPre, "Edit poll")
	// The fresh form row carries no "no" button and unmarked cells stay
	// neutral (no st-r tint) — the red ✗ appears only in the
	// submitted-answers rows.
	absent(bPre, "mark-r")
	absent(bPre, "form-cell st-r")
	contains(bPre, `class="cell form-cell"`)
	contains(bPre, `data-i18n="meet.legend"`)
	if got := r.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options: %q, want DENY", got)
	}
	if !strings.Contains(r.Header.Get("Content-Security-Policy"), "frame-ancestors") {
		t.Fatalf("missing frame-ancestors CSP: %q", r.Header.Get("Content-Security-Policy"))
	}

	// The admin page carries both copyable links, the edit button, and
	// mints its own identity (the organizer answers through it too).
	rAdm := p.get("/meet/"+adminA, "")
	status(rAdm, http.StatusOK)
	adminTok := p.cookie(rAdm, cookie)
	if adminTok == "" || adminTok == alice {
		t.Fatalf("admin view must mint a distinct identity, got %q (alice %q)", adminTok, alice)
	}
	bAdm := p.body(rAdm)
	for _, want := range []string{
		"Edit poll", "Respondent link", "Admin link",
		`href="/meet-form/edit/` + adminA + `"`,
		"/meet/" + mA.Key, "/meet/" + adminA,
	} {
		if !strings.Contains(bAdm, want) {
			t.Fatalf("admin page missing %q", want)
		}
	}

	// Answering without a valid identity+token pair is rejected, and a
	// non-form body never reaches the handlers.
	for _, tc := range []struct {
		name, form, cookie string
	}{
		{"no cookie", "name=Alice&csrftoken=x", ""},
		{"cookie without token", "name=Alice", cookie + "=" + alice},
		{"cookie with wrong token", "name=Alice&csrftoken=deadbeef", cookie + "=" + alice},
	} {
		if r := p.post("/meet/"+mA.Key+"/answer", tc.form, tc.cookie); r.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: expected 401, got %d", tc.name, r.StatusCode)
		}
	}
	if r := p.do(http.MethodPost, "/meet/"+mA.Key+"/answer", `{"name":"X"}`, "", "application/json"); r.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("json body: expected 415, got %d", r.StatusCode)
	}

	// Alice answers: one slot available, one maybe.
	rAns := p.post("/meet/"+mA.Key+"/answer",
		"name=Alice&csrftoken="+csrfToken(alice)+"&a"+s1+"=g&a"+s2+"=y", cookie+"="+alice)
	status(rAns, http.StatusSeeOther)
	if loc := rAns.Header.Get("Location"); loc != "/meet/"+mA.Key {
		t.Fatalf("answer redirect: %q", loc)
	}
	gotA, err := db.Get(mA.Key)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if len(gotA.Answers) != 1 || gotA.Answers[0].ID != alice || gotA.Answers[0].Name != "Alice" ||
		gotA.Answers[0].Marks[s1] != "g" || gotA.Answers[0].Marks[s2] != "y" {
		t.Fatalf("answer not stored as submitted: %+v", gotA.Answers)
	}
	// The submitted answer renders as a static row: name with the
	// edit/delete icons in front, tinted marks per slot. The "you" marker
	// is a client-swappable i18n span (data-i18n="meet.you").
	bOwn := p.body(p.get("/meet/"+mA.Key, cookie+"="+alice))
	contains(bOwn, "Alice")
	contains(bOwn, `data-i18n="meet.you"`)
	contains(bOwn, `?edit=`+alice)
	contains(bOwn, `data-id="`+alice+`"`)
	contains(bOwn, "st-g")
	contains(bOwn, "st-y")

	// Bob arrives: a distinct identity, whose answer Alice can see (the
	// poll does not hide respondents).
	bob := p.cookie(p.get("/meet/"+mA.Key, ""), cookie)
	if bob == alice {
		t.Fatalf("second visitor must get a fresh identity")
	}
	status(p.post("/meet/"+mA.Key+"/answer",
		"name=Bob&csrftoken="+csrfToken(bob)+"&a"+s1+"=g&a"+s2+"=g", cookie+"="+bob), http.StatusSeeOther)
	bMut := p.body(p.get("/meet/"+mA.Key, cookie+"="+alice))
	contains(bMut, "Alice")
	contains(bMut, "Bob")
	// The totals rows sit below every answer row — always the table's
	// last rows. Per slot: slot 1 has two yes (Alice + Bob), slot 2 one
	// yes + one maybe — the yes row highlights only slot 1, the yes +
	// maybe row (2, 2) both. Structural assertions on the parsed table.
	pMut := parsePage(t, bMut)
	tableShape(t, pMut)
	mrows := tbodyRows(answersTable(t, pMut))
	if len(mrows) < 2 {
		t.Fatalf("answers table: not enough rows for the totals block")
	}
	mtrYes, mtrYM := mrows[len(mrows)-2], mrows[len(mrows)-1]
	if !hasClass(mtrYes, "totals-row") || !hasClass(mtrYM, "totals-row") {
		t.Fatalf("the totals rows must be the table's last rows")
	}
	// The label cell: a localized "Total" prefix + the mark's symbol in
	// its mark color, a title tooltip naming the mark (incl. the
	// combined row's two glyphs).
	contains(bMut, `data-i18n="totals.total"`)
	contains(bMut, `class="totals-sym"`)
	contains(bMut, `title="Yes"`)
	// html/template escapes the + in the attribute value (&#43; renders
	// as a plain + in the tooltip).
	contains(bMut, `title="Yes &#43; Maybe"`)
	contains(bMut, `data-i18n-title="totals.yes_maybe"`)
	contains(bMut, `class="totals-sym2"`)
	// Yes: 2 then 1, only the max cell highlighted; yes + maybe: 2 then
	// 2, both cells at the max, both highlighted.
	cYes, cYM := rowTds(mtrYes), rowTds(mtrYM)
	if got := nodeText(cYes[1]) + " " + nodeText(cYes[2]); got != "2 1" {
		t.Fatalf("yes totals row: %q, want [2 1]", got)
	}
	if got := nodeText(cYM[1]) + " " + nodeText(cYM[2]); got != "2 2" {
		t.Fatalf("yes + maybe totals row: %q, want [2 2]", got)
	}
	if got := hotCells(mtrYes) + hotCells(mtrYM); got != 3 {
		t.Fatalf("totals: %d highlighted cells, want 3", got)
	}
	if !hasClass(cYes[1], "totals-hot") || hasClass(cYes[2], "totals-hot") ||
		!hasClass(cYM[1], "totals-hot") || !hasClass(cYM[2], "totals-hot") {
		t.Fatalf("totals highlight wrong: yes must light only slot 1, combined must light both")
	}

	// ---- poll B: hidden respondents ----
	mB, _ := p.createPoll("title=Poll+B&email=poll%40example.com&description=B+description&timezone=Europe/Berlin&enddate=" + endA + "&hide-users=true&slot0=2026-11-01T10:00")
	cb := cookieName(mB)
	carol := p.cookie(p.get("/meet/"+mB.Key, ""), cb)
	status(p.post("/meet/"+mB.Key+"/answer", "name=Carol&csrftoken="+csrfToken(carol)+"&a"+s1+"=g", cb+"="+carol), http.StatusSeeOther)
	contains(p.body(p.get("/meet/"+mB.Key, cb+"="+carol)), "Carol")
	stranger := p.cookie(p.get("/meet/"+mB.Key, ""), cb)
	absent(p.body(p.get("/meet/"+mB.Key, cb+"="+stranger)), "Carol")
	adminB := p.cookie(p.get("/meet/"+mB.AdminKey, ""), cb)
	contains(p.body(p.get("/meet/"+mB.AdminKey, cb+"="+adminB)), "Carol")
	// With hidden respondents the totals rows are on the admin page
	// only — the user page shows none.
	absent(p.body(p.get("/meet/"+mB.Key, cb+"="+carol)), "totals-row")
	contains(p.body(p.get("/meet/"+mB.AdminKey, cb+"="+adminB)), "totals-row")

	// ---- poll C: single-slot signups ----
	mC, _ := p.createPoll("title=Poll+C&email=poll%40example.com&description=C+description&timezone=Europe/Berlin&enddate=" + endA + "&single-signup=true&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00")
	cc := cookieName(mC)
	cy := p.cookie(p.get("/meet/"+mC.Key, ""), cc)
	bC := p.body(p.get("/meet/"+mC.Key, cc+"="+cy))
	contains(bC, `data-single="true"`)
	absent(bC, "mark-y")
	absent(bC, "mark-r")
	absent(bC, "form-cell st-r")
	// A single-signup poll carries no totals rows at all.
	absent(bC, "totals-row")
	postC := func(form string) *http.Response {
		return p.post("/meet/"+mC.Key+"/answer", form, cc+"="+cy)
	}
	rC1 := postC("name=Cy&csrftoken=" + csrfToken(cy) + "&a" + s1 + "=x")
	status(rC1, http.StatusBadRequest)
	contains(p.body(rC1), "Invalid answer option")
	status(postC("name=Cy&csrftoken="+csrfToken(cy)+"&a"+s1+"=g&a"+s2+"=g"), http.StatusBadRequest)
	status(postC("name=Cy&csrftoken="+csrfToken(cy)+"&a"+s2+"=y"), http.StatusBadRequest)
	status(postC("name=&csrftoken="+csrfToken(cy)+"&a"+s1+"=g"), http.StatusBadRequest)
	status(postC("name=Cy&csrftoken="+csrfToken(cy)+"&a"+s1+"=g"), http.StatusSeeOther)
	gotC, err := db.Get(mC.Key)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if len(gotC.Answers) != 1 || gotC.Answers[0].Name != "Cy" || len(gotC.Answers[0].Marks) != 1 || gotC.Answers[0].Marks[s1] != "g" {
		t.Fatalf("single-slot answer stored wrong: %+v", gotC.Answers)
	}
	// The unmarked slot renders red with an ✗ in the result matrix —
	// "no" is the absence of a mark, and the display keeps the red.
	contains(p.body(p.get("/meet/"+mC.Key, cc+"="+cy)), `td class="cell st-r"`)

	// ---- back to poll A: editing the own answer ----
	status(p.post("/meet/"+mA.Key+"/answer",
		"name=Alice+J&csrftoken="+csrfToken(alice)+"&a"+s1+"=g", cookie+"="+alice), http.StatusSeeOther)
	gotA, _ = db.Get(mA.Key)
	if len(gotA.Answers) != 2 {
		t.Fatalf("editing must not add an answer: %d answers", len(gotA.Answers))
	}
	aliceAns := findAnswer(gotA.Answers, alice)
	if aliceAns == nil || aliceAns.Name != "Alice J" || aliceAns.Marks[s1] != "g" {
		t.Fatalf("own edit stored wrong: %+v", aliceAns)
	}
	if _, ok := aliceAns.Marks[s2]; ok {
		t.Fatalf("unmarked slot must drop from the marks: %+v", aliceAns.Marks)
	}
	bEd := p.body(p.get("/meet/"+mA.Key, cookie+"="+alice))
	contains(bEd, "Alice J")
	contains(bEd, `data-i18n="meet.you"`)
	absent(bEd, "st-y")

	// ... and deleting it, which also expires the identity cookie.
	rDel := p.post("/meet/"+mA.Key+"/answer/delete",
		"respondentid="+alice+"&csrftoken="+csrfToken(alice), cookie+"="+alice)
	status(rDel, http.StatusSeeOther)
	dropped := false
	for _, sc := range rDel.Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, cookie+"=") && strings.Contains(sc, "expires=Thu, 01 Jan 1970") {
			dropped = true
		}
	}
	if !dropped {
		t.Fatalf("deleting the own answer must expire the identity cookie: %v", rDel.Header.Values("Set-Cookie"))
	}
	gotA, _ = db.Get(mA.Key)
	if findAnswer(gotA.Answers, alice) != nil {
		t.Fatalf("own answer not removed: %+v", gotA.Answers)
	}
	// A deleted answer leaves only the fresh form row behind.
	bFresh := p.body(p.get("/meet/"+mA.Key, cookie+"="+alice))
	contains(bFresh, `placeholder="Your name"`)
	contains(bFresh, "Submit answer")
	absent(bFresh, "Alice J")

	// A user link may never act on someone else's answer.
	status(p.post("/meet/"+mA.Key+"/answer/delete",
		"respondentid="+alice+"&csrftoken="+csrfToken(bob), cookie+"="+bob), http.StatusNotFound)
	status(p.post("/meet/"+mA.Key+"/answer/delete",
		"respondentid=ghost&csrftoken="+csrfToken(bob), cookie+"="+bob), http.StatusNotFound)

	// ---- the admin edits any answer (admin link only) ----
	status(p.post("/meet/"+adminA+"/answer/edit",
		"respondentid="+bob+"&name=Bobby&a"+s1+"=&a"+s2+"=g", ""), http.StatusSeeOther)
	gotA, _ = db.Get(mA.Key)
	bobAns := findAnswer(gotA.Answers, bob)
	if bobAns == nil || bobAns.Name != "Bobby" || bobAns.Marks[s2] != "g" || len(bobAns.Marks) != 1 {
		t.Fatalf("admin edit stored wrong: %+v", bobAns)
	}
	status(p.post("/meet/"+mA.Key+"/answer/edit",
		"respondentid="+bob+"&name=X", ""), http.StatusNotFound)
	status(p.post("/meet/"+adminA+"/answer/edit",
		"respondentid=ghost&name=X", ""), http.StatusNotFound)

	// ... and the organizer's own answer, placed through the admin view,
	// is editable from both views. The admin view posts through the admin
	// link, so the back-redirect lands on the admin page.
	rOwnAdm := p.post("/meet/"+adminA+"/answer",
		"name=Organizer&csrftoken="+csrfToken(adminTok)+"&a"+s1+"=g", cookie+"="+adminTok)
	status(rOwnAdm, http.StatusSeeOther)
	if loc := rOwnAdm.Header.Get("Location"); loc != "/meet/"+adminA {
		t.Fatalf("organizer answer redirect: %q", loc)
	}
	gotA, _ = db.Get(mA.Key)
	if findAnswer(gotA.Answers, adminTok) == nil {
		t.Fatalf("organizer answer not stored: %+v", gotA.Answers)
	}
	contains(p.body(p.get("/meet/"+adminA, cookie+"="+adminTok)), `data-i18n="meet.you"`) // the organizer's own row is marked with the i18n "you" span
	// Both views post their answer forms through their own link, so the
	// back-redirect lands on the page the form came from; the organizer's
	// row carries the edit/delete icons in both.
	bAdmView := p.body(p.get("/meet/"+adminA, cookie+"="+adminTok))
	bUsrView := p.body(p.get("/meet/"+mA.Key, cookie+"="+adminTok))
	contains(bAdmView, `action="/meet/`+adminA+`/answer"`)
	absent(bAdmView, `action="/meet/`+mA.Key+`/answer"`)
	contains(bUsrView, `action="/meet/`+mA.Key+`/answer"`)
	absent(bUsrView, `action="/meet/`+adminA+`/answer"`)
	contains(bAdmView, `?edit=`+adminTok)
	contains(bUsrView, `?edit=`+adminTok)
	contains(bUsrView, `data-id="`+adminTok+`"`)
	// The admin view's delete-own form is a capability post through the
	// admin link and keeps the admin on the admin page.
	rOwnDel := p.post("/meet/"+adminA+"/answer/delete", "respondentid="+adminTok, cookie+"="+adminTok)
	status(rOwnDel, http.StatusSeeOther)
	if loc := rOwnDel.Header.Get("Location"); loc != "/meet/"+adminA {
		t.Fatalf("organizer own-answer delete redirect: %q", loc)
	}
	gotA, _ = db.Get(mA.Key)
	if findAnswer(gotA.Answers, adminTok) != nil {
		t.Fatalf("organizer answer not removed: %+v", gotA.Answers)
	}

	// The edit row renders as the table's second-to-last row (the form,
	// next to the save/cancel bar below the answers, the totals rows
	// below it), posting through the admin edit endpoint with their id
	// and prefilled name; their old position shows neither a static row
	// nor an edit icon.
	bEdRow := p.body(p.get("/meet/"+adminA+"?edit="+bob, cookie+"="+adminTok))
	contains(bEdRow, `action="/meet/`+adminA+`/answer/edit"`)
	contains(bEdRow, `name="respondentid" value="`+bob+`"`)
	contains(bEdRow, `value="Bobby"`)
	contains(bEdRow, "Save changes")
	absent(bEdRow, `?edit=`+bob)
	pEdRow := parsePage(t, bEdRow)
	tableShape(t, pEdRow)
	edRows := tbodyRows(answersTable(t, pEdRow))
	if len(edRows) < 3 || !hasClass(edRows[len(edRows)-2], "totals-row") || !hasClass(edRows[len(edRows)-1], "totals-row") {
		t.Fatalf("edit view: the totals rows must be the table's last rows")
	}
	// The row directly above the totals block (one borderless gap row
	// separates it from the form) is the prefilled edit row.
	iForm := len(edRows) - 3
	if hasClass(edRows[iForm], "totals-gap") {
		iForm--
	}
	if iForm < 0 || !hasClass(edRows[iForm], "form-row") {
		t.Fatalf("edit view: the form row must sit directly above the table's totals block")
	}
	// A user link may never open someone else's row, and an unknown
	// ?edit target just renders the plain page.
	bUsrEd := p.body(p.get("/meet/"+mA.Key+"?edit="+bob, cookie+"="+adminTok))
	absent(bUsrEd, `answer/edit`)
	absent(bUsrEd, `value="Bobby"`)
	absent(bUsrEd, `data-id="`+bob+`"`)
	bGhost := p.body(p.get("/meet/"+adminA+"?edit=ghost", cookie+"="+adminTok))
	absent(bGhost, `answer/edit`)
	contains(bGhost, "Submit answer")

	// ---- editing the poll itself ----
	rEd := p.get("/meet-form/edit/"+adminA, "")
	status(rEd, http.StatusOK)
	bEdForm := p.body(rEd)
	for _, want := range []string{
		`value="Poll A"`, `value="` + adminA + `"`, `value="` + endA + `"`,
		`<option value="Europe/Berlin" selected>`, "Pick a time",
		// the edit page is GET-only; the form must POST to /meet-form
		// (without the action, a browser edit lands on 404)
		`action="/meet-form"`,
		"href=\"/meet/" + adminA + "\" data-i18n=\"form.cancel\"",
	} {
		if !strings.Contains(bEdForm, want) {
			t.Fatalf("edit form missing %q", want)
		}
	}
	status(p.get("/meet-form/edit/"+mA.Key, ""), http.StatusNotFound)

	// A new slot set, a new title, a new end date — and both links must
	// survive, with marks for removed slots pruned from the answers.
	rPe := p.post("/meet-form", "meetingkey="+adminA+
		"&title=Poll+A+v2&email=poll%40example.com&description=Updated&timezone=Europe/Berlin&enddate="+endAEd+"&slot0=2026-11-01T12:00", "")
	status(rPe, http.StatusSeeOther)
	if loc := rPe.Header.Get("Location"); loc != "/meet/"+adminA {
		t.Fatalf("poll edit redirect: %q", loc)
	}
	gotA, _ = db.Get(mA.Key)
	if gotA.Key != mA.Key || gotA.AdminKey != adminA {
		t.Fatalf("poll edit must keep both links: %q / %q", gotA.Key, gotA.AdminKey)
	}
	if gotA.Title != "Poll A v2" {
		t.Fatalf("title not updated: %q", gotA.Title)
	}
	if !gotA.Expires.Equal(expAEd) {
		t.Fatalf("expiry not updated: %v", gotA.Expires)
	}
	if len(gotA.Dates) != 1 || !gotA.Dates[0].Equal(time.Date(2026, 11, 1, 12, 0, 0, 0, berlin)) {
		t.Fatalf("slots not updated: %v", gotA.Dates)
	}
	for i := range gotA.Answers {
		if len(gotA.Answers[i].Marks) != 0 {
			t.Fatalf("marks for removed slots must be pruned: %+v", gotA.Answers)
		}
	}

	// ---- poll D: expiry and prolongation ----
	mD, adminD := p.createPoll("title=Poll+D&email=poll%40example.com&description=D+description&timezone=Europe/Berlin&enddate=" + endD + "&slot0=2026-11-01T10:00")
	// Force the poll into the past, derived from now so it stays past.
	past := time.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -30)
	mD.Expires = past
	if err := db.Update(mD); err != nil {
		t.Fatalf("forcing expiry: %v", err)
	}
	cd := cookieName(mD)
	rEnd := p.get("/meet/"+mD.Key, "")
	status(rEnd, http.StatusOK)
	bEnd := p.body(rEnd)
	contains(bEnd, "This poll has ended")
	// the expiry date renders as a data-slot f-date span; the "on " prefix
	// lives inside the i18n meet.ended span, so we assert the span alone.
	contains(bEnd, `<span class="f-date" data-slot="`+slotKey(past)+`" data-style="long">`+past.In(berlin).Format("2006-01-02")+`</span>`)
	absent(bEnd, `id="answer-form"`)
	if p.hasCookie(rEnd, cd) {
		t.Fatalf("no identity cookie may be minted for an ended poll")
	}
	status(p.post("/meet/"+mD.Key+"/answer", "name=X", ""), http.StatusGone)
	status(p.post("/meet/"+mD.AdminKey+"/answer", "name=X", ""), http.StatusGone)
	bAdmEnd := p.body(p.get("/meet/"+adminD, ""))
	contains(bAdmEnd, "This poll has ended")
	contains(bAdmEnd, "Edit poll")

	// The organizer can prolong an ended poll, reopening answering.
	rDp := p.post("/meet-form", "meetingkey="+adminD+
		"&title=Poll+D&email=poll%40example.com&description=D+description&timezone=Europe/Berlin&enddate="+endDP+"&slot0=2026-11-01T10:00", "")
	status(rDp, http.StatusSeeOther)
	if loc := rDp.Header.Get("Location"); loc != "/meet/"+adminD {
		t.Fatalf("prolongation redirect: %q", loc)
	}
	gotD, err := db.Get(mD.Key)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !gotD.Expires.Equal(expDP) {
		t.Fatalf("prolongation not stored: %v", gotD.Expires)
	}
	rReopen := p.get("/meet/"+mD.Key, "")
	contains(p.body(rReopen), `id="answer-form"`)
	if !p.hasCookie(rReopen, cd) {
		t.Fatalf("reopened poll must mint identity cookies again")
	}

	// ---- poll A: close, re-open, delete ----
	// The admin page offers closing and deleting, not re-opening; the
	// respondent page carries neither.
	bClAdm := p.body(p.get("/meet/"+adminA, ""))
	contains(bClAdm, "Close poll")
	contains(bClAdm, `action="/meet/`+adminA+`/close"`)
	absent(bClAdm, `Re-open poll`)
	contains(bClAdm, `Delete poll`)
	bClUser := p.body(p.get("/meet/"+mA.Key, ""))
	absent(bClUser, `Close poll`)
	absent(bClUser, `Delete poll`)
	// A user key can never close the poll.
	status(p.post("/meet/"+mA.Key+"/close", "", ""), http.StatusNotFound)
	// Closing stops the answers.
	status(p.post("/meet/"+adminA+"/close", "", ""), http.StatusSeeOther)
	gotA, err = db.Get(mA.Key)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !gotA.Closed {
		t.Fatalf("poll must be closed after close: %+v", gotA)
	}
	if gotA.ClosedAt == nil || gotA.ClosedAt.IsZero() {
		t.Fatalf("closing must stamp the closure moment: %+v", gotA)
	}
	// The respondent page shows the closed notice and the answers table
	// (without the form), and mints no new identity cookie.
	rCl := p.get("/meet/"+mA.Key, "")
	status(rCl, http.StatusOK)
	bClRsp := p.body(rCl)
	// The closed notice carries the closure moment as a datetime, and the
	// meta line's deletion date moves to closure + retention (earlier than
	// the end date's).
	contains(bClRsp, `data-i18n="meet.closed"`)
	contains(bClRsp, `data-style="datetime"`)
	contains(bClRsp, `data-slot="`+slotKey(gotA.DeletionDate(cfg.FinishedTTL))+`"`)
	contains(bClRsp, `Bobby`)
	absent(bClRsp, `id="answer-form"`)
	absent(bClRsp, `id="del-form"`)
	if p.hasCookie(rCl, cookie) {
		t.Fatalf("no identity cookie may be minted for a closed poll")
	}
	// Answering, editing, and deleting are all refused while closed.
	status(p.post("/meet/"+mA.Key+"/answer",
		"name=Late&csrftoken="+csrfToken(bob), cookie+"="+bob), http.StatusGone)
	status(p.post("/meet/"+adminA+"/answer/edit", "respondentid="+bob+"&name=Late", ""), http.StatusGone)
	status(p.post("/meet/"+adminA+"/answer/delete", "respondentid="+bob, cookie+"="+adminTok), http.StatusGone)
	// The admin page now offers re-opening, not closing.
	bRo := p.body(p.get("/meet/"+adminA, ""))
	contains(bRo, `Re-open poll`)
	contains(bRo, `action="/meet/`+adminA+`/reopen"`)
	absent(bRo, `action="/meet/`+adminA+`/close"`)

	// ---- final slots: the closed poll's decision, marked by the admin ----
	// (poll A was edited down to one slot earlier — use the slot it
	// offers now; an open poll refusing the mark is covered by the vote
	// suite.)
	sA := slotKey(gotA.Dates[0])
	// A user key can never mark finals, and a missing/unknown slot is
	// rejected.
	status(p.post("/meet/"+mA.Key+"/final", "slot="+sA, ""), http.StatusNotFound)
	status(p.post("/meet/"+adminA+"/final", "", ""), http.StatusBadRequest)
	status(p.post("/meet/"+adminA+"/final", "slot=2020-01-01T00:00Z07:00", ""), http.StatusNotFound)
	status(p.post("/meet/"+adminA+"/final", "slot="+sA, ""), http.StatusSeeOther)
	gotA, _ = db.Get(mA.Key)
	if len(gotA.FinalSlots) != 1 || slotKey(gotA.FinalSlots[0]) != sA {
		t.Fatalf("final slot not stored: %+v", gotA.FinalSlots)
	}
	// The admin page carries the footer row (the "Choose final slot(s):"
	// label in the name column, the one slot's toggle button pressed),
	// the highlighted header (both the date and time cells of the single
	// slot column) — and, while a final is set, the two-step re-open
	// warning instead of a plain submit.
	bFinAdm := p.body(p.get("/meet/"+adminA, ""))
	for _, want := range []string{
		`data-i18n="meet.finalChoose"`,
		`class="th-time final" data-slot="` + sA + `"`,
		`class="final-btn" form="final-form" name="slot" value="` + sA + `" aria-pressed="true"`,
		`class="final-footer-row"`,
		`id="final-form"`,
		`class="btn-secondary reopen-poll-btn"`,
		`class="reopen-warning"`,
	} {
		if !strings.Contains(bFinAdm, want) {
			t.Fatalf("admin final view missing %q", want)
		}
	}
	// The user link sees the highlight (header, body cells tinted the
	// respondent's mark class, the "Final!" label in the footer row)
	// without the toggle buttons or the warning.
	bFinUser := p.body(p.get("/meet/"+mA.Key, ""))
	contains(bFinUser, `class="th-time final" data-slot="`+sA+`"`)
	contains(bFinUser, `class="final-footer-row"`)
	contains(bFinUser, `class="final-mark-text"`)
	if bobAns := findAnswer(gotA.Answers, bob); bobAns != nil {
		cls := map[string]string{"g": "g", "y": "y"}[bobAns.Marks[sA]]
		if cls == "" {
			cls = "r"
		}
		contains(bFinUser, `cell st-`+cls+` final`)
	}
	for _, gone := range []string{`final-btn`, `final-form`, `reopen-warning`} {
		absent(bFinUser, gone)
	}
	// Toggling the same slot off clears it — then set it again, so the
	// re-open below has a final mark to clear.
	status(p.post("/meet/"+adminA+"/final", "slot="+sA, ""), http.StatusSeeOther)
	gotA, _ = db.Get(mA.Key)
	if len(gotA.FinalSlots) != 0 {
		t.Fatalf("toggling must clear the final slot: %+v", gotA.FinalSlots)
	}
	status(p.post("/meet/"+adminA+"/final", "slot="+sA, ""), http.StatusSeeOther)

	// A single-signup poll gets no final feature at all.
	_, adminS := p.createPoll("title=Poll+F&email=poll%40example.com&description=F&timezone=Europe/Berlin&enddate=" + endA + "&single-signup=true&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00")
	status(p.post("/meet/"+adminS+"/close", "", ""), http.StatusSeeOther)
	bSAdm := p.body(p.get("/meet/"+adminS, ""))
	for _, gone := range []string{`final-btn`, `final-form`, `final-footer-row`} {
		absent(bSAdm, gone)
	}
	status(p.post("/meet/"+adminS+"/final", "slot="+s1, ""), http.StatusBadRequest)

	// Re-opening (admin key only) lets the answers flow again — and clears
	// the final marks.
	status(p.post("/meet/"+mA.Key+"/reopen", "", ""), http.StatusNotFound)
	status(p.post("/meet/"+adminA+"/reopen", "", ""), http.StatusSeeOther)
	gotA, _ = db.Get(mA.Key)
	if gotA.Closed {
		t.Fatalf("poll must be open after re-open: %+v", gotA)
	}
	if gotA.ClosedAt != nil {
		t.Fatalf("re-opening must clear the closure stamp: %+v", gotA)
	}
	if len(gotA.FinalSlots) != 0 {
		t.Fatalf("re-opening must clear the final marks: %+v", gotA.FinalSlots)
	}
	slotA := slotKey(gotA.Dates[0])
	status(p.post("/meet/"+mA.Key+"/answer",
		"name=Late&csrftoken="+csrfToken(bob)+"&a"+slotA+"=g", cookie+"="+bob), http.StatusSeeOther)
	gotA, _ = db.Get(mA.Key)
	if a := findAnswer(gotA.Answers, bob); a == nil || a.Name != "Late" || a.Marks[slotA] != "g" {
		t.Fatalf("a reopened poll must accept answers again: %+v", gotA.Answers)
	}

	// Deleting purges the poll: the row, the answers, both links.
	status(p.post("/meet/"+mA.Key+"/delete", "", ""), http.StatusNotFound)
	rDelPoll := p.post("/meet/"+adminA+"/delete", "", cookie+"="+adminTok)
	status(rDelPoll, http.StatusSeeOther)
	if loc := rDelPoll.Header.Get("Location"); loc != "/" {
		t.Fatalf("delete redirect: %q", loc)
	}
	// The respondent identities go with the poll: the cookie is expired.
	cookieDropped := false
	for _, sc := range rDelPoll.Header.Values("Set-Cookie") {
		if strings.HasPrefix(sc, cookie+"=") && strings.Contains(sc, "expires=Thu, 01 Jan 1970") {
			cookieDropped = true
		}
	}
	if !cookieDropped {
		t.Fatalf("deleting the poll must expire the identity cookie: %v", rDelPoll.Header.Values("Set-Cookie"))
	}
	if _, err := db.Get(mA.Key); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("poll row not deleted (user key): %v", err)
	}
	if _, err := db.GetAdmin(adminA); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("poll row not deleted (admin key): %v", err)
	}
	// Both links and the edit form now render the gone page — a 404 with
	// its own message, which the plain 404 of an unknown path lacks.
	for _, path := range []string{"/meet/" + mA.Key, "/meet/" + adminA, "/meet-form/edit/" + adminA} {
		rGone := p.get(path, "")
		status(rGone, http.StatusNotFound)
		bGone := p.body(rGone)
		contains(bGone, `data-i18n="meet.goneTitle"`)
		contains(bGone, `kiusan | Poll not found`)
	}
	rPlain := p.get("/definitely-missing", "")
	status(rPlain, http.StatusNotFound)
	absent(p.body(rPlain), `meet.goneTitle`)

	// The create route enforces the same content-type gate.
	if r := p.do(http.MethodPost, "/meet-form", `{"title":"X"}`, "", "application/json"); r.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("json create: expected 415, got %d", r.StatusCode)
	}
}

// mustZone loads an IANA zone, failing the test when the (bundled) zoneinfo
// cannot provide it.
func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("loading zone %s: %v", name, err)
	}
	return loc
}

func TestPollSqlite(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	runPollSuite(t)
}

// Enabling single-signup on a poll that already collects answers
// invalidates every multi-slot answer: the edit wipes all respondents
// (answers and their stored identities) so everyone starts from a fresh
// poll, and re-saving with the option already on keeps the new answers.
func TestMeetEditSingleSignupWipesAnswers(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	app := newTestApp(t)
	p := newPollClient(t, app)
	berlin := mustZone(t, "Europe/Berlin")
	s1 := slotKey(time.Date(2026, 11, 1, 10, 0, 0, 0, berlin))
	s2 := slotKey(time.Date(2026, 11, 1, 14, 0, 0, 0, berlin))
	endS, _ := testEndDate(berlin)

	m, admin := p.createPoll("title=Poll+S&email=poll%40example.com&description=Pick+a+time&timezone=Europe/Berlin&enddate=" + endS + "&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00")
	cookie := cookieName(m)
	alice := p.cookie(p.get("/meet/"+m.Key, ""), cookie)
	if r := p.post("/meet/"+m.Key+"/answer",
		"name=Alice&csrftoken="+csrfToken(alice)+"&a"+s1+"=g&a"+s2+"=g", cookie+"="+alice); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("answer: expected 303, got %d", r.StatusCode)
	}
	before, err := db.Get(m.Key)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if len(before.Answers) != 1 || len(before.Answers[0].Marks) != 2 {
		t.Fatalf("answers before edit: %+v", before.Answers)
	}

	// Edit: enable single-signup, slots and the rest unchanged.
	rEd := p.post("/meet-form", "meetingkey="+admin+
		"&title=Poll+S&email=poll%40example.com&description=Pick+a+time&timezone=Europe/Berlin&enddate="+endS+"&single-signup=true&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00", "")
	if rEd.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit: expected 303, got %d", rEd.StatusCode)
	}
	got, err := db.Get(m.Key)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if !got.SingleSignup {
		t.Fatalf("single-signup not stored")
	}
	if len(got.Answers) != 0 {
		t.Fatalf("wiping answers must remove every respondent (incl. stored identities): %+v", got.Answers)
	}
	if got.Key != m.Key || got.AdminKey != admin {
		t.Fatalf("wiping answers must keep both links: %q / %q", got.Key, got.AdminKey)
	}

	// Answering a wiped poll starts fresh: one single-slot answer.
	bob := p.cookie(p.get("/meet/"+m.Key, ""), cookie)
	if r := p.post("/meet/"+m.Key+"/answer",
		"name=Bob&csrftoken="+csrfToken(bob)+"&a"+s1+"=g", cookie+"="+bob); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("fresh answer: expected 303, got %d", r.StatusCode)
	}
	fresh, _ := db.Get(m.Key)
	if len(fresh.Answers) != 1 {
		t.Fatalf("fresh answer not stored: %+v", fresh.Answers)
	}

	// Re-saving with single-signup already on keeps the single answers.
	if r := p.post("/meet-form", "meetingkey="+admin+
		"&title=Poll+S v2&email=poll%40example.com&description=Pick+a+time&timezone=Europe/Berlin&enddate="+endS+"&single-signup=true&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00", ""); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("re-save: expected 303, got %d", r.StatusCode)
	}
	kept, _ := db.Get(m.Key)
	if len(kept.Answers) != 1 {
		t.Fatalf("re-save with single-signup already on must keep answers: %+v", kept.Answers)
	}
}

// A closed poll's final marks are pruned when the poll is edited: a slot
// that disappears takes its final mark with it, and enabling
// single-signup wipes the final marks along with the answers.
func TestMeetFinalSlotsPrunedOnEdit(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	app := newTestApp(t)
	p := newPollClient(t, app)
	berlin := mustZone(t, "Europe/Berlin")
	s1 := slotKey(time.Date(2026, 11, 1, 10, 0, 0, 0, berlin))
	s2 := slotKey(time.Date(2026, 11, 1, 14, 0, 0, 0, berlin))
	endS, _ := testEndDate(berlin)

	m, admin := p.createPoll("title=Poll+P&email=poll%40example.com&description=Pick+a+time&timezone=Europe/Berlin&enddate=" + endS + "&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00")
	// Close the poll and mark both slots final.
	if r := p.post("/meet/"+admin+"/close", "", ""); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("close: expected 303, got %d", r.StatusCode)
	}
	for _, s := range []string{s1, s2} {
		if r := p.post("/meet/"+admin+"/final", "slot="+s, ""); r.StatusCode != http.StatusSeeOther {
			t.Fatalf("final %s: expected 303, got %d", s, r.StatusCode)
		}
	}
	before, err := db.Get(m.Key)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if len(before.FinalSlots) != 2 {
		t.Fatalf("finals not stored: %+v", before.FinalSlots)
	}

	// Editing down to a single slot keeps only that slot's final mark.
	if r := p.post("/meet-form", "meetingkey="+admin+
		"&title=Poll+P&email=poll%40example.com&description=Pick+a+time&timezone=Europe/Berlin&enddate="+endS+"&slot0=2026-11-01T14:00", ""); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit: expected 303, got %d", r.StatusCode)
	}
	after, _ := db.Get(m.Key)
	if len(after.FinalSlots) != 1 || slotKey(after.FinalSlots[0]) != s2 {
		t.Fatalf("edit must prune the removed slot's final mark: %+v", after.FinalSlots)
	}

	// Enabling single-signup wipes the final marks along with the answers.
	if r := p.post("/meet-form", "meetingkey="+admin+
		"&title=Poll+P&email=poll%40example.com&description=Pick+a+time&timezone=Europe/Berlin&enddate="+endS+"&single-signup=true&slot0=2026-11-01T14:00", ""); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("edit single-signup: expected 303, got %d", r.StatusCode)
	}
	single, _ := db.Get(m.Key)
	if len(single.FinalSlots) != 0 {
		t.Fatalf("single-signup must wipe the final marks: %+v", single.FinalSlots)
	}
}

// TestMeetICS drives the .ics download through both link shapes: a
// single-signup poll offers the viewer's own marked slot (identity cookie
// read, not minted), a closed regular poll offers its final slots to
// everyone, and re-opening clears them again.
func TestMeetICS(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	app := newTestApp(t)
	p := newPollClient(t, app)
	berlin := mustZone(t, "Europe/Berlin")
	end, _ := testEndDate(berlin)

	status := func(r *http.Response, code int) {
		p.t.Helper()
		if r.StatusCode != code {
			p.t.Fatalf("expected %d, got %d", code, r.StatusCode)
		}
	}

	// ---- single-signup: the viewer's own slot ----
	s0 := slotKey(time.Date(2027, 1, 1, 8, 0, 0, 0, berlin))
	s1 := slotKey(time.Date(2027, 1, 2, 9, 30, 0, 0, berlin))
	m, admin := p.createPoll("title=1on1&email=poll%40example.com&description=Pair+time&timezone=Europe/Berlin&enddate=" + end + "&single-signup=true&slot0=2027-01-01T08:00&slot1=2027-01-02T09:30")
	cookie := cookieName(m)

	// No stored answer: no download, and the page advertises none.
	if r := p.get("/meet/"+m.Key+"/ics", ""); r.StatusCode != http.StatusNotFound {
		t.Fatalf("no cookie: expected 404, got %d", r.StatusCode)
	}
	r := p.get("/meet/"+m.Key, "")
	status(r, http.StatusOK)
	tok := p.cookie(r, cookie)
	if strings.Contains(p.body(r), `data-i18n="meet.icsDownload"`) {
		t.Fatalf("the page must not offer a download before an answer exists")
	}

	// Answer through the user link, then download with that identity.
	status(p.post("/meet/"+m.Key+"/answer", "name=Ana&csrftoken="+csrfToken(tok)+"&a"+s0+"=g", cookie+"="+tok), http.StatusSeeOther)
	r = p.get("/meet/"+m.Key+"/ics", cookie+"="+tok)
	status(r, http.StatusOK)
	if ct := r.Header.Get("Content-Type"); ct != "text/calendar; charset=utf-8" {
		t.Fatalf("content type: %q", ct)
	}
	if cd := r.Header.Get("Content-Disposition"); cd != `attachment; filename="1on1.ics"` {
		t.Fatalf("disposition: %q", cd)
	}
	b := p.body(r)
	for _, want := range []string{
		"BEGIN:VCALENDAR",
		"SUMMARY:1on1",
		"DESCRIPTION:Pair time",
		"DTSTART;TZID=Europe/Berlin:20270101T080000",
		"DTEND;TZID=Europe/Berlin:20270101T080000",
		"UID:kiusan/" + m.Key + "/" + s0,
	} {
		if !strings.Contains(b, want) {
			t.Fatalf("ics missing %q:\n%s", want, b)
		}
	}
	if n := strings.Count(b, "BEGIN:VEVENT"); n != 1 {
		t.Fatalf("expected exactly one event, got %d:\n%s", n, b)
	}
	if p.hasCookie(r, cookie) {
		t.Fatalf("the download must not mint an identity cookie")
	}

	// Both links now carry the download (each on its own link).
	for _, key := range []string{m.Key, admin} {
		pr := p.get("/meet/"+key, cookie+"="+tok)
		status(pr, http.StatusOK)
		body := p.body(pr)
		for _, want := range []string{
			`data-i18n="meet.icsDownload"`,
			"href=\"/meet/" + key + "/ics\"",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("the %s page misses %q", key, want)
			}
		}
	}

	// A stranger identity owns no slot.
	if r := p.get("/meet/"+m.Key+"/ics", cookie+"=someone-else"); r.StatusCode != http.StatusNotFound {
		t.Fatalf("a stranger identity: expected 404, got %d", r.StatusCode)
	}

	// The organizer answers through the admin link and gets their own slot.
	ar := p.get("/meet/"+admin, "")
	status(ar, http.StatusOK)
	atok := p.cookie(ar, cookie)
	status(p.post("/meet/"+admin+"/answer", "name=Bob&csrftoken="+csrfToken(atok)+"&a"+s1+"=g", cookie+"="+atok), http.StatusSeeOther)
	if r := p.get("/meet/"+admin+"/ics", cookie+"="+atok); r.StatusCode != http.StatusOK {
		t.Fatalf("admin ics: expected 200, got %d", r.StatusCode)
	} else if ab := p.body(r); !strings.Contains(ab, "DTSTART;TZID=Europe/Berlin:20270102T093000") {
		t.Fatalf("the admin's ics must carry their own slot:\n%s", ab)
	}

	// ---- regular: the final slots, public while closed ----
	m2, admin2 := p.createPoll("title=ICS+sync&email=poll%40example.com&description=&timezone=Europe/Berlin&enddate=" + end + "&slot0=2027-02-01T10:00&slot1=2027-02-02&slot2=2027-02-03T14:30")
	sA := slotKey(time.Date(2027, 2, 1, 10, 0, 0, 0, berlin))
	sB := slotKey(time.Date(2027, 2, 2, 0, 0, 0, 0, berlin))

	// Open: no finals, no download.
	if r := p.get("/meet/"+m2.Key+"/ics", ""); r.StatusCode != http.StatusNotFound {
		t.Fatalf("open regular poll: expected 404, got %d", r.StatusCode)
	}
	status(p.post("/meet/"+admin2+"/close", "", ""), http.StatusSeeOther)
	for _, s := range []string{sA, sB} {
		status(p.post("/meet/"+admin2+"/final", "slot="+s, ""), http.StatusSeeOther)
	}

	// The finals are public: no cookie needed, and both links download them
	// (a date-only final renders as a zero-length all-day event).
	for _, key := range []string{m2.Key, admin2} {
		r = p.get("/meet/"+key+"/ics", "")
		status(r, http.StatusOK)
		if cd := r.Header.Get("Content-Disposition"); cd != `attachment; filename="ICS-sync.ics"` {
			t.Fatalf("disposition: %q", cd)
		}
		b := p.body(r)
		if n := strings.Count(b, "BEGIN:VEVENT"); n != 2 {
			t.Fatalf("expected the two final slots as events, got %d:\n%s", n, b)
		}
		for _, want := range []string{
			"SUMMARY:ICS sync",
			"DTSTART;TZID=Europe/Berlin:20270201T100000",
			"DTSTART;VALUE=DATE:20270202",
			"DTEND;VALUE=DATE:20270202",
		} {
			if !strings.Contains(b, want) {
				t.Fatalf("ics missing %q:\n%s", want, b)
			}
		}
		if i := strings.Index(b, "20270201T100000"); i < 0 || strings.Index(b, "20270202") < i {
			t.Fatalf("the final slots must be chronological:\n%s", b)
		}
		// The page advertises the download on this link.
		pb := p.body(p.get("/meet/"+key, ""))
		if !strings.Contains(pb, "href=\"/meet/"+key+"/ics\"") {
			t.Fatalf("the %s page misses the ics link", key)
		}
	}

	// Re-opening clears the finals and with them the download.
	status(p.post("/meet/"+admin2+"/reopen", "", ""), http.StatusSeeOther)
	if r := p.get("/meet/"+m2.Key+"/ics", ""); r.StatusCode != http.StatusNotFound {
		t.Fatalf("after re-open: expected 404, got %d", r.StatusCode)
	}
}
