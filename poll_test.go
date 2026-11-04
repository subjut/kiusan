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
	r := p.post("/1-1-form", form, "")
	if r.StatusCode != http.StatusSeeOther {
		p.t.Fatalf("POST /1-1-form: expected 303, got %d", r.StatusCode)
	}
	loc := r.Header.Get("Location")
	adminKey := strings.TrimPrefix(loc, "/meet/")
	if adminKey == "" || adminKey == loc {
		p.t.Fatalf("POST /1-1-form: unexpected Location %q", loc)
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
	mA, adminA := p.createPoll("title=Poll+A&email=poll%40example.com&description=Pick+a+time&timezone=Europe/Berlin&enddate=2026-12-31&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00")
	if mA.Key == "" || mA.AdminKey != adminA {
		t.Fatalf("poll A keys: user %q, admin %q (Location %q)", mA.Key, mA.AdminKey, adminA)
	}
	if got, err := db.Get(adminA); err == nil || got != nil {
		t.Fatalf("admin key must not resolve as a user key (err %v)", err)
	} else if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("admin key lookup: %v", err)
	}
	if wantEnd := time.Date(2026, 12, 31, 23, 59, 59, 0, berlin); !mA.Expires.Equal(wantEnd) {
		t.Fatalf("poll A expiry: got %v, want %v", mA.Expires, wantEnd)
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
		`href="/1-1-form/edit/` + adminA + `"`,
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

	// ---- poll B: hidden respondents ----
	mB, _ := p.createPoll("title=Poll+B&email=poll%40example.com&description=B+description&timezone=Europe/Berlin&enddate=2026-12-31&hide-users=true&slot0=2026-11-01T10:00")
	cb := cookieName(mB)
	carol := p.cookie(p.get("/meet/"+mB.Key, ""), cb)
	status(p.post("/meet/"+mB.Key+"/answer", "name=Carol&csrftoken="+csrfToken(carol)+"&a"+s1+"=g", cb+"="+carol), http.StatusSeeOther)
	contains(p.body(p.get("/meet/"+mB.Key, cb+"="+carol)), "Carol")
	stranger := p.cookie(p.get("/meet/"+mB.Key, ""), cb)
	absent(p.body(p.get("/meet/"+mB.Key, cb+"="+stranger)), "Carol")
	adminB := p.cookie(p.get("/meet/"+mB.AdminKey, ""), cb)
	contains(p.body(p.get("/meet/"+mB.AdminKey, cb+"="+adminB)), "Carol")

	// ---- poll C: single-slot signups ----
	mC, _ := p.createPoll("title=Poll+C&email=poll%40example.com&description=C+description&timezone=Europe/Berlin&enddate=2026-12-31&single-signup=true&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00")
	cc := cookieName(mC)
	cy := p.cookie(p.get("/meet/"+mC.Key, ""), cc)
	bC := p.body(p.get("/meet/"+mC.Key, cc+"="+cy))
	contains(bC, `data-single="true"`)
	absent(bC, "mark-y")
	absent(bC, "mark-r")
	absent(bC, "form-cell st-r")
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

	// The edit row renders in place: the opened respondent's row becomes
	// the form, posting through the admin edit endpoint with their id and
	// prefilled name.
	bEdRow := p.body(p.get("/meet/"+adminA+"?edit="+bob, cookie+"="+adminTok))
	contains(bEdRow, `action="/meet/`+adminA+`/answer/edit"`)
	contains(bEdRow, `name="respondentid" value="`+bob+`"`)
	contains(bEdRow, `value="Bobby"`)
	contains(bEdRow, "Save changes")
	absent(bEdRow, `?edit=`+bob)
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
	rEd := p.get("/1-1-form/edit/"+adminA, "")
	status(rEd, http.StatusOK)
	bEdForm := p.body(rEd)
	for _, want := range []string{
		`value="Poll A"`, `value="` + adminA + `"`, `value="2026-12-31"`,
		`<option value="Europe/Berlin" selected>`, "Pick a time",
		// the edit page is GET-only; the form must POST to /1-1-form
		// (without the action, a browser edit lands on 404)
		`action="/1-1-form"`,
	} {
		if !strings.Contains(bEdForm, want) {
			t.Fatalf("edit form missing %q", want)
		}
	}
	status(p.get("/1-1-form/edit/"+mA.Key, ""), http.StatusNotFound)

	// A new slot set, a new title, a new end date — and both links must
	// survive, with marks for removed slots pruned from the answers.
	rPe := p.post("/1-1-form", "meetingkey="+adminA+
		"&title=Poll+A+v2&email=poll%40example.com&description=Updated&timezone=Europe/Berlin&enddate=2027-01-31&slot0=2026-11-01T12:00", "")
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
	if wantEnd := time.Date(2027, 1, 31, 23, 59, 59, 0, berlin); !gotA.Expires.Equal(wantEnd) {
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
	mD, adminD := p.createPoll("title=Poll+D&email=poll%40example.com&description=D+description&timezone=Europe/Berlin&enddate=2026-12-31&slot0=2026-11-01T10:00")
	mD.Expires = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Update(mD); err != nil {
		t.Fatalf("forcing expiry: %v", err)
	}
	cd := cookieName(mD)
	rEnd := p.get("/meet/"+mD.Key, "")
	status(rEnd, http.StatusOK)
	bEnd := p.body(rEnd)
	contains(bEnd, "This poll has ended")
	// the expiry date renders as a data-slot f-date span; the "on " prefix
	// lives inside the i18n meet.endedPre span, so we assert the span alone.
	contains(bEnd, `<span class="f-date" data-slot="2026-10-01T00:00Z" data-style="long">2026-10-01</span>`)
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
	rDp := p.post("/1-1-form", "meetingkey="+adminD+
		"&title=Poll+D&email=poll%40example.com&description=D+description&timezone=Europe/Berlin&enddate=2027-03-01&slot0=2026-11-01T10:00", "")
	status(rDp, http.StatusSeeOther)
	if loc := rDp.Header.Get("Location"); loc != "/meet/"+adminD {
		t.Fatalf("prolongation redirect: %q", loc)
	}
	gotD, err := db.Get(mD.Key)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if wantEnd := time.Date(2027, 3, 1, 23, 59, 59, 0, berlin); !gotD.Expires.Equal(wantEnd) {
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
	// The respondent page shows the closed notice and the answers table
	// (without the form), and mints no new identity cookie.
	rCl := p.get("/meet/"+mA.Key, "")
	status(rCl, http.StatusOK)
	bClRsp := p.body(rCl)
	contains(bClRsp, `data-i18n="meet.closedPre"`)
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
	// Re-opening (admin key only) lets the answers flow again.
	status(p.post("/meet/"+mA.Key+"/reopen", "", ""), http.StatusNotFound)
	status(p.post("/meet/"+adminA+"/reopen", "", ""), http.StatusSeeOther)
	gotA, _ = db.Get(mA.Key)
	if gotA.Closed {
		t.Fatalf("poll must be open after re-open: %+v", gotA)
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
	for _, path := range []string{"/meet/" + mA.Key, "/meet/" + adminA, "/1-1-form/edit/" + adminA} {
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
	if r := p.do(http.MethodPost, "/1-1-form", `{"title":"X"}`, "", "application/json"); r.StatusCode != http.StatusUnsupportedMediaType {
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
