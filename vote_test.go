package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
)

// postVoteForm submits a url-encoded body to /choice-form, like a browser does.
func postVoteForm(t *testing.T, app *fiber.App, form string) (*http.Response, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/choice-form", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return app.Test(req)
}

func TestVoteFormRender(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)

	r, err := app.Test(httptest.NewRequest(http.MethodGet, "/choice-form", nil))
	if err != nil {
		t.Fatalf("GET /choice-form: %v", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET /choice-form: status %d", r.StatusCode)
	}
	b, _ := io.ReadAll(r.Body)
	body := string(b)

	for _, want := range []string{
		`id="form"`,
		`name="title"`,
		`name="email"`,
		`name="description"`,
		`name="hide-users"`,
		`name="single-choice"`,
		`name="free-text"`,
		`name="enddate"`,
		`id="choice-rows"`,
		`name="choice0"`,
		`id="add-choice"`,
		`id="choice-count"`,
		`src="/chooser.js"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /choice-form: missing %q in body", want)
		}
	}
}

func TestVoteFormCreate(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)

	form := url.Values{
		"title":       {"Team Dinner"},
		"email":       {"team@example.com"},
		"description": {"Pick a restaurant"},
		"choice0":     {"Pizza place"},
		"choice1":     {"Sushi bar"},
		"choice2":     {"Burger joint"},
		"enddate":     {"2026-12-31"},
	}.Encode()

	r, err := postVoteForm(t, app, form)
	if err != nil {
		t.Fatalf("POST /choice-form: %v", err)
	}
	if r.StatusCode != http.StatusSeeOther && r.StatusCode != http.StatusFound {
		t.Fatalf("POST /choice-form: status %d, want redirect", r.StatusCode)
	}
	loc := r.Header.Get("Location")
	if !strings.HasPrefix(loc, "/vote/") {
		t.Fatalf("POST /choice-form: redirect %q, want /vote/...", loc)
	}

	// Follow the redirect to the admin page (the vote page doesn't exist
	// yet, so just verify the redirect target is well-formed).
	key := strings.TrimPrefix(loc, "/vote/")
	if len(key) != 16 {
		t.Errorf("admin key %q: want 16 chars, got %d", key, len(key))
	}
}

func TestVoteFormValidation(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)

	cases := []struct {
		name string
		form string
	}{
		{"no choices", url.Values{
			"title": {"T"}, "email": {"e@e.com"}, "enddate": {"2026-12-31"},
		}.Encode()},
		{"title too long", url.Values{
			"title":   {strings.Repeat("a", 101)},
			"email":   {"e@e.com"},
			"choice0": {"A"},
			"enddate": {"2026-12-31"},
		}.Encode()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := postVoteForm(t, app, tc.form)
			if err != nil {
				t.Fatalf("POST /choice-form: %v", err)
			}
			if r.StatusCode != http.StatusBadRequest {
				t.Fatalf("POST /choice-form (%s): status %d, want 400", tc.name, r.StatusCode)
			}
		})
	}
}

func TestVoteFormEdit(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)

	// Create a vote first.
	form := url.Values{
		"title":         {"Edit Test"},
		"email":         {"edit@example.com"},
		"description":   {"Testing edit"},
		"choice0":       {"Option A"},
		"choice1":       {"Option B"},
		"single-choice": {"true"},
		"free-text":     {"true"},
		"hide-users":    {"true"},
		"enddate":       {"2026-12-31"},
	}.Encode()

	r, err := postVoteForm(t, app, form)
	if err != nil {
		t.Fatalf("POST /choice-form: %v", err)
	}
	loc := r.Header.Get("Location")
	adminKey := strings.TrimPrefix(loc, "/vote/")

	// Load the edit form.
	r2, err := app.Test(httptest.NewRequest(http.MethodGet, "/choice-form/edit/"+adminKey, nil))
	if err != nil {
		t.Fatalf("GET /choice-form/edit/%s: %v", adminKey, err)
	}
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("GET /choice-form/edit: status %d", r2.StatusCode)
	}
	b, _ := io.ReadAll(r2.Body)
	body := string(b)

	for _, want := range []string{
		`name="votekey" value="` + adminKey + `"`,
		`value="Edit Test"`,
		`value="edit@example.com"`,
		`value="Option A"`,
		`value="Option B"`,
		`name="single-choice" value="true" checked`,
		`name="free-text" value="true" checked`,
		`name="hide-users" value="true" checked`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /choice-form/edit: missing %q in body", want)
		}
	}
}

func TestVoteFormEditNotFound(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)

	r, err := app.Test(httptest.NewRequest(http.MethodGet, "/choice-form/edit/nonexistentkey1234", nil))
	if err != nil {
		t.Fatalf("GET /choice-form/edit/unknown: %v", err)
	}
	if r.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /choice-form/edit/unknown: status %d, want 404", r.StatusCode)
	}
}

// createVote posts a vote form and returns both link keys (user, admin).
func createVote(t *testing.T, app *fiber.App, extra url.Values) (string, string) {
	t.Helper()
	form := url.Values{
		"title":   {"V"},
		"email":   {"v@e.com"},
		"choice0": {"Option A"},
		"choice1": {"Option B"},
		"enddate": {"2026-12-31"},
	}
	for k, v := range extra {
		form[k] = v
	}
	r, err := postVoteForm(t, app, form.Encode())
	if err != nil {
		t.Fatalf("POST /choice-form: %v", err)
	}
	if r.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /choice-form: status %d", r.StatusCode)
	}
	adminKey := strings.TrimPrefix(r.Header.Get("Location"), "/vote/")
	v, err := db.GetVoteAdmin(adminKey)
	if err != nil {
		t.Fatalf("loading created vote: %v", err)
	}
	return v.Key, adminKey
}

// identityFromCookies extracts the minted vote identity cookie value.
func identityFromCookies(t *testing.T, r *http.Response, userKey string) string {
	t.Helper()
	for _, c := range r.Cookies() {
		if c.Name == "kiusan-"+userKey {
			return c.Value
		}
	}
	t.Fatalf("no identity cookie %q in response", "kiusan-"+userKey)
	return ""
}

// voteChoices loads a vote by its user key and returns its choices (ID +
// text).
func voteChoices(t *testing.T, userKey string) database.ChoiceList {
	t.Helper()
	v, err := db.GetVote(userKey)
	if err != nil {
		t.Fatalf("loading vote: %v", err)
	}
	return v.Choices
}

// submitAnswer posts one voter's answer to the vote and returns the
// response.
func submitAnswer(t *testing.T, app *fiber.App, userKey, identity string, form url.Values) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	r, err := app.Test(req)
	if err != nil {
		t.Fatalf("POST /vote/answer: %v", err)
	}
	return r
}

// answerVote loads the vote page (minting the identity cookie) and
// submits the voter's answer: the "a<ID>" marks or "n<ID>" notes under
// the exact form keys, plus the name and the CSRF token. It expects the
// 303.
func answerVote(t *testing.T, app *fiber.App, userKey, name string, answer map[string]string) {
	t.Helper()
	r, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	if err != nil {
		t.Fatalf("GET /vote: %v", err)
	}
	identity := identityFromCookies(t, r, userKey)
	form := url.Values{"name": {name}, "csrftoken": {csrfToken(identity)}}
	for k, v := range answer {
		form.Set(k, v)
	}
	if r2 := submitAnswer(t, app, userKey, identity, form); r2.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /vote/answer: status %d", r2.StatusCode)
	}
}

// editVote posts a vote edit (the form with the votekey hidden field set):
// a minimal poll body and one choiceN row per entry, each entry optionally
// carrying the choiceID the edit form rendered, and returns the saved vote.
func editVote(t *testing.T, app *fiber.App, adminKey string, rows []choiceRow) *database.Vote {
	t.Helper()
	// The edit form pre-checks the mode boxes from the stored vote — the
	// submission mirrors that.
	old, err := db.GetVoteAdmin(adminKey)
	if err != nil {
		t.Fatalf("loading vote for edit: %v", err)
	}
	form := url.Values{
		"title":   {"V"},
		"email":   {"v@e.com"},
		"enddate": {"2026-12-31"},
		"votekey": {adminKey},
	}
	if old.FreeText {
		form.Set("free-text", "true")
	}
	if old.SingleChoice {
		form.Set("single-choice", "true")
	}
	if old.HideUsers {
		form.Set("hide-users", "true")
	}
	for i, r := range rows {
		form.Set("choice"+strconv.Itoa(i), r.text)
		if r.oldID != "" {
			form.Set("choiceID"+strconv.Itoa(i), r.oldID)
		}
	}
	resp, err := postVoteForm(t, app, form.Encode())
	if err != nil {
		t.Fatalf("POST /choice-form (edit): %v", err)
	}
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /choice-form (edit): status %d, want redirect", resp.StatusCode)
	}
	v, err := db.GetVoteAdmin(adminKey)
	if err != nil {
		t.Fatalf("loading edited vote: %v", err)
	}
	return v
}

func TestVotePageRender(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, adminKey := createVote(t, app, nil)

	// Admin view: links section, mode label, the choice header, the
	// three-button mark stack, the legend.
	r, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+adminKey, nil))
	if err != nil {
		t.Fatalf("GET /vote/admin: %v", err)
	}
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET /vote/admin: status %d", r.StatusCode)
	}
	b, _ := io.ReadAll(r.Body)
	body := string(b)
	for _, want := range []string{
		`class="th-choice">Option A`,
		`class="th-choice">Option B`,
		`vote.modeMarks`,
		`class="legend"`,
		`class="links"`,
		`class="copybox"`,
		`class="mark mark-g"`,
		`class="mark mark-r"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("GET /vote/admin: missing %q", want)
		}
	}
	if got := strings.Count(body, `class="mark mark-g"`); got < 1 {
		t.Errorf("GET /vote/admin: expected mark-g buttons in the form row")
	}
	if got := strings.Count(body, `class="mark mark-r"`); got < 1 {
		t.Errorf("GET /vote/admin: expected mark-r buttons in the form row")
	}

	// User view: no links section, form row with the name input.
	r2, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	if err != nil {
		t.Fatalf("GET /vote/user: %v", err)
	}
	if r2.StatusCode != http.StatusOK {
		t.Fatalf("GET /vote/user: status %d", r2.StatusCode)
	}
	b2, _ := io.ReadAll(r2.Body)
	body2 := string(b2)
	if strings.Contains(body2, `class="links"`) {
		t.Errorf("GET /vote/user: the links section must not render")
	}
	if !strings.Contains(body2, `name="name"`) {
		t.Errorf("GET /vote/user: missing the name input")
	}

	// Unknown key: the gone page.
	r3, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/nonexistentkey1234", nil))
	if err != nil {
		t.Fatalf("GET /vote/unknown: %v", err)
	}
	if r3.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /vote/unknown: status %d, want 404", r3.StatusCode)
	}
}

func TestVoteAnswerRoundTrip(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, _ := createVote(t, app, nil)
	choices := voteChoices(t, userKey)

	// First page load mints the identity cookie.
	r, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	if err != nil {
		t.Fatalf("GET /vote: %v", err)
	}
	identity := identityFromCookies(t, r, userKey)

	form := url.Values{
		"name":              {"Ada"},
		"a" + choices[0].ID: {"y"},
		"csrftoken":         {csrfToken(identity)},
	}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	r2, err := app.Test(req)
	if err != nil {
		t.Fatalf("POST /vote/answer: %v", err)
	}
	if r2.StatusCode != http.StatusSeeOther {
		t.Fatalf("POST /vote/answer: status %d, want redirect", r2.StatusCode)
	}

	v, err := db.GetVote(userKey)
	if err != nil {
		t.Fatalf("loading vote: %v", err)
	}
	if len(v.Answers) != 1 || v.Answers[0].Name != "Ada" {
		t.Fatalf("answers after submit: %+v", v.Answers)
	}
	if v.Answers[0].Marks[choices[0].ID] != "y" {
		t.Errorf("mark for %q: %q, want y", choices[0].Text, v.Answers[0].Marks[choices[0].ID])
	}
	if _, ok := v.Answers[0].Marks[choices[1].ID]; ok {
		t.Errorf("mark for %q: present, want absent (neutral)", choices[1].Text)
	}

	// The submitted row renders the glyph; the unmarked cell stays neutral.
	r3, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	if err != nil {
		t.Fatalf("GET /vote after answer: %v", err)
	}
	b3, _ := io.ReadAll(r3.Body)
	body3 := string(b3)
	if !strings.Contains(body3, "Ada") {
		t.Errorf("GET /vote after answer: missing the voter name")
	}
	if !strings.Contains(body3, `st-g`) {
		t.Errorf("GET /vote after answer: missing the green yes cell")
	}

	// No CSRF: rejected.
	reqBad := httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(url.Values{
		"name": {"Mallory"}, "a" + choices[0].ID: {"y"},
	}.Encode()))
	reqBad.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	reqBad.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	r4, err := app.Test(reqBad)
	if err != nil {
		t.Fatalf("POST /vote/answer (no csrf): %v", err)
	}
	if r4.StatusCode != http.StatusUnauthorized {
		t.Fatalf("POST /vote/answer (no csrf): status %d, want 401", r4.StatusCode)
	}
}

func TestVoteSingleChoice(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, _ := createVote(t, app, url.Values{"single-choice": {"true"}})
	choices := voteChoices(t, userKey)

	r, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	if err != nil {
		t.Fatalf("GET /vote: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	s := string(body)
	if strings.Contains(s, `class="mark mark-y"`) {
		t.Errorf("single-choice vote: the maybe button must not render")
	}
	// The legend offers no maybe in a single-choice vote.
	if !strings.Contains(s, `vote.legendSingle`) {
		t.Errorf("single-choice vote: missing the single-choice legend")
	}
	if strings.Contains(s, "(\u2713) maybe") {
		t.Errorf("single-choice vote: the legend must not offer maybe")
	}
	// Every form cell starts on no (red tint, pressed no button).
	if !strings.Contains(s, `data-status="n"`) || !strings.Contains(s, `value="n"`) {
		t.Errorf("single-choice vote: form cells must default to no")
	}

	identity := identityFromCookies(t, r, userKey)

	// Two yes: rejected server-side.
	formTwo := url.Values{
		"name": {"Two"}, "a" + choices[0].ID: {"y"}, "a" + choices[1].ID: {"y"}, "csrftoken": {csrfToken(identity)},
	}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(formTwo))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	if r2, _ := app.Test(req); r2.StatusCode != http.StatusBadRequest {
		t.Fatalf("two yes: status %d, want 400", r2.StatusCode)
	}

	// One yes, one empty: the empty one is stored as no.
	formOne := url.Values{
		"name": {"Ada"}, "a" + choices[0].ID: {"y"}, "a" + choices[1].ID: {""}, "csrftoken": {csrfToken(identity)},
	}.Encode()
	req = httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(formOne))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	if r2, err := app.Test(req); err != nil || r2.StatusCode != http.StatusSeeOther {
		t.Fatalf("one yes: %v status %d", err, r2.StatusCode)
	}
	v, _ := db.GetVote(userKey)
	if v.Answers[0].Marks[choices[1].ID] != "n" {
		t.Errorf("unvoted option: %q, want n (normalized)", v.Answers[0].Marks[choices[1].ID])
	}

	// The submitted row shows the red no glyph for the unvoted option.
	r3, _ := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	b3, _ := io.ReadAll(r3.Body)
	if !strings.Contains(string(b3), `st-r`) {
		t.Errorf("single-choice row: missing the red no cell")
	}
}

func TestVoteFreeText(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, _ := createVote(t, app, url.Values{"free-text": {"true"}})
	choices := voteChoices(t, userKey)

	r, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	if err != nil {
		t.Fatalf("GET /vote: %v", err)
	}
	body, _ := io.ReadAll(r.Body)
	s := string(body)
	if !strings.Contains(s, `vote.modeFreeText`) {
		t.Errorf("free-text vote: missing the mode label")
	}
	if strings.Contains(s, `class="legend"`) {
		t.Errorf("free-text vote: the mark legend must not render")
	}
	if !strings.Contains(s, `name="n`+choices[0].ID+`" maxlength="150"`) {
		t.Errorf("free-text vote: missing the per-option text inputs")
	}

	identity := identityFromCookies(t, r, userKey)

	// One filled note (free-text without single-choice allows several).
	form := url.Values{
		"name": {"Ada"}, "n" + choices[0].ID: {"works for me"}, "csrftoken": {csrfToken(identity)},
	}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	if r2, err := app.Test(req); err != nil || r2.StatusCode != http.StatusSeeOther {
		t.Fatalf("free-text answer: %v status %d", err, r2.StatusCode)
	}
	v, _ := db.GetVote(userKey)
	if v.Answers[0].Notes[choices[0].ID] != "works for me" {
		t.Errorf("note for %q: %q", choices[0].Text, v.Answers[0].Notes[choices[0].ID])
	}

	// The note renders in the submitted row.
	r3, _ := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	b3, _ := io.ReadAll(r3.Body)
	if !strings.Contains(string(b3), "works for me") {
		t.Errorf("free-text row: missing the stored note")
	}

	// A note over 150 chars: rejected.
	long := make([]rune, 151)
	for i := range long {
		long[i] = 'a'
	}
	formLong := url.Values{
		"name": {"Ada"}, "n" + choices[0].ID: {string(long)}, "csrftoken": {csrfToken(identity)},
	}.Encode()
	req = httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(formLong))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	if r4, _ := app.Test(req); r4.StatusCode != http.StatusBadRequest {
		t.Fatalf("long note: status %d, want 400", r4.StatusCode)
	}
}

func TestVoteSingleChoiceFreeText(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, _ := createVote(t, app, url.Values{"free-text": {"true"}, "single-choice": {"true"}})
	choices := voteChoices(t, userKey)

	r, err := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	if err != nil {
		t.Fatalf("GET /vote: %v", err)
	}
	identity := identityFromCookies(t, r, userKey)

	// Two filled notes: rejected.
	formTwo := url.Values{
		"name": {"Two"}, "n" + choices[0].ID: {"a"}, "n" + choices[1].ID: {"b"}, "csrftoken": {csrfToken(identity)},
	}.Encode()
	req := httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(formTwo))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	if r2, _ := app.Test(req); r2.StatusCode != http.StatusBadRequest {
		t.Fatalf("two notes: status %d, want 400", r2.StatusCode)
	}

	// One note: stored; the unvoted option stays an empty cell (free-text
	// has no stored "no" to render).
	formOne := url.Values{
		"name": {"Ada"}, "n" + choices[0].ID: {"a"}, "csrftoken": {csrfToken(identity)},
	}.Encode()
	req = httptest.NewRequest(http.MethodPost, "/vote/"+userKey+"/answer", strings.NewReader(formOne))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Cookie", "kiusan-"+userKey+"="+identity)
	if r2, err := app.Test(req); err != nil || r2.StatusCode != http.StatusSeeOther {
		t.Fatalf("one note: %v status %d", err, r2.StatusCode)
	}
	r3, _ := app.Test(httptest.NewRequest(http.MethodGet, "/vote/"+userKey, nil))
	b3, _ := io.ReadAll(r3.Body)
	s3 := string(b3)
	if !strings.Contains(s3, "a") {
		t.Errorf("single-choice free-text row: missing the stored note")
	}
	if strings.Contains(s3, `st-r`) {
		t.Errorf("single-choice free-text row: the unvoted option must not render as no")
	}
}

func TestVoteEditOptionTextChanged(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, adminKey := createVote(t, app, nil)
	choices := voteChoices(t, userKey)

	// Yes to the first option, no to the second.
	answerVote(t, app, userKey, "Ada", map[string]string{
		"a" + choices[0].ID: "y",
		"a" + choices[1].ID: "n",
	})

	// Edit: change the first option's text (the row keeps the ID the form
	// rendered), leave the second untouched.
	edited := editVote(t, app, adminKey, []choiceRow{
		{text: "Option A2", oldID: choices[0].ID},
		{text: "Option B", oldID: choices[1].ID},
	})
	if edited.Choices[0].ID == choices[0].ID {
		t.Errorf("edited option kept its old ID")
	}
	if edited.Choices[1].ID != choices[1].ID {
		t.Errorf("unchanged option lost its ID")
	}
	if len(edited.Answers) != 1 {
		t.Fatalf("answers after edit: %+v", edited.Answers)
	}
	marks := edited.Answers[0].Marks
	if marks[edited.Choices[1].ID] != "n" {
		t.Errorf("unchanged option's answer lost: %v", marks)
	}
	if _, ok := marks[choices[0].ID]; ok {
		t.Errorf("edited option's answer survived: %v", marks)
	}
	if len(marks) != 1 {
		t.Errorf("unexpected marks after edit: %v", marks)
	}
}

func TestVoteEditDeleteOptionKeepsAnswers(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, adminKey := createVote(t, app, nil)
	choices := voteChoices(t, userKey)

	// Yes to the first option, maybe to the second.
	answerVote(t, app, userKey, "Ada", map[string]string{
		"a" + choices[0].ID: "y",
		"a" + choices[1].ID: "m",
	})

	// Edit: delete the first option. Under index keying, deleting a row
	// shifted the remaining options' marks onto the deleted option's.
	edited := editVote(t, app, adminKey, []choiceRow{
		{text: "Option B", oldID: choices[1].ID},
	})
	if len(edited.Choices) != 1 || edited.Choices[0].ID != choices[1].ID {
		t.Fatalf("choices after delete: %+v", edited.Choices)
	}
	if len(edited.Answers) != 1 {
		t.Fatalf("answers after delete: %+v", edited.Answers)
	}
	marks := edited.Answers[0].Marks
	if marks[choices[1].ID] != "m" {
		t.Errorf("remaining option's answer: %v, want m under %q", marks, choices[1].ID)
	}
	if len(marks) != 1 {
		t.Errorf("marks after delete: %v, want only the remaining option's", marks)
	}
}

func TestVoteEditReorderOptions(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, adminKey := createVote(t, app, nil)
	choices := voteChoices(t, userKey)

	answerVote(t, app, userKey, "Ada", map[string]string{
		"a" + choices[0].ID: "y",
		"a" + choices[1].ID: "m",
	})

	// Edit: swap the options' order (the rows carry their IDs).
	edited := editVote(t, app, adminKey, []choiceRow{
		{text: "Option B", oldID: choices[1].ID},
		{text: "Option A", oldID: choices[0].ID},
	})
	if edited.Choices[0].ID != choices[1].ID || edited.Choices[1].ID != choices[0].ID {
		t.Fatalf("reorder did not carry the IDs: %+v", edited.Choices)
	}
	marks := edited.Answers[0].Marks
	if marks[choices[0].ID] != "y" {
		t.Errorf("answer for %q after reorder: %v", choices[0].Text, marks)
	}
	if marks[choices[1].ID] != "m" {
		t.Errorf("answer for %q after reorder: %v", choices[1].Text, marks)
	}
}

func TestVoteEditFreeTextOption(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	app := newTestApp(t)
	userKey, adminKey := createVote(t, app, url.Values{"free-text": {"true"}})
	choices := voteChoices(t, userKey)

	// Fill both notes.
	answerVote(t, app, userKey, "Ada", map[string]string{
		"n" + choices[0].ID: "works",
		"n" + choices[1].ID: "only on Fridays",
	})

	// Edit: change the first option's text.
	edited := editVote(t, app, adminKey, []choiceRow{
		{text: "Option A2", oldID: choices[0].ID},
		{text: "Option B", oldID: choices[1].ID},
	})
	if len(edited.Answers) != 1 {
		t.Fatalf("answers after edit: %+v", edited.Answers)
	}
	notes := edited.Answers[0].Notes
	if notes[edited.Choices[1].ID] != "only on Fridays" {
		t.Errorf("unchanged option's note lost: %v", notes)
	}
	if _, ok := notes[choices[0].ID]; ok {
		t.Errorf("edited option's note survived: %v", notes)
	}
	if len(notes) != 1 {
		t.Errorf("unexpected notes after edit: %v", notes)
	}
}
