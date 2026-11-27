package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
)

// probeForm runs fn with the live request context of a url-encoded form
// POST, so the form-reading helpers (collectMarks, collectVoteAnswers,
// isFormPost, absURL) can be exercised without booting the whole app.
func probeForm(t *testing.T, body, contentType string, fn func(c fiber.Ctx)) {
	t.Helper()
	app := fiber.New()
	app.Post("/probe", func(c fiber.Ctx) error {
		fn(c)
		return c.SendString("ok")
	})
	req := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if _, err := app.Test(req); err != nil {
		t.Fatalf("app.Test: %v", err)
	}
}

// ---- the pure helpers ----

func TestRowMax(t *testing.T) {
	cases := []struct {
		in   []int
		want int
	}{
		{nil, 0},
		{[]int{}, 0},
		{[]int{0, 0, 0}, 0},
		{[]int{5}, 5},
		{[]int{3, 9, 1, 9, 2}, 9},
	}
	for _, c := range cases {
		if got := rowMax(c.in); got != c.want {
			t.Fatalf("rowMax(%v): %d, want %d", c.in, got, c.want)
		}
	}
}

func TestSlotKey(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("loading zone: %v", err)
	}
	cases := []struct {
		in   time.Time
		want string
	}{
		// the slot's absolute instant, seconds stripped, always UTC
		{time.Date(2026, 11, 1, 10, 0, 0, 0, berlin), "2026-11-01T09:00Z"},
		{time.Date(2026, 11, 1, 23, 59, 59, 0, time.UTC), "2026-11-01T23:59Z"},
		{time.Date(2026, 1, 1, 0, 0, 0, 0, time.FixedZone("+02:00", 2*3600)), "2025-12-31T22:00Z"},
	}
	for _, c := range cases {
		if got := slotKey(c.in); got != c.want {
			t.Fatalf("slotKey(%v): %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseEndDate(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		raw  string
		want time.Time
		msg  string
	}{
		{"empty is the 3-month default", "", now.AddDate(0, 3, 0), ""},
		{"today is allowed", "2026-06-15", time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC), ""},
		{"whitespace is trimmed", "  2026-07-01  ", time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), ""},
		{"yesterday is past", "2026-06-14", time.Time{}, "End date is in the past"},
		{"swapped month/day is invalid", "15-06-2026", time.Time{}, "Invalid end date"},
		{"impossible day is invalid", "2026-02-30", time.Time{}, "Invalid end date"},
		{"the 5-year horizon is inclusive", "2031-06-15", time.Date(2031, 6, 15, 0, 0, 0, 0, time.UTC), ""},
		{"beyond the horizon", "2031-06-16", time.Time{}, "End date is too far in the future"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, msg := parseEndDate(c.raw, now)
			if msg != c.msg || !got.Equal(c.want) {
				t.Fatalf("parseEndDate(%q): (%v, %q), want (%v, %q)", c.raw, got, msg, c.want, c.msg)
			}
		})
	}
}

func TestEndOf(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("loading zone: %v", err)
	}
	// the last instant of the calendar day in the given zone
	for _, loc := range []*time.Location{time.UTC, berlin} {
		got := endOf(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), loc)
		want := time.Date(2026, 11, 1, 23, 59, 59, 0, loc)
		if !got.Equal(want) {
			t.Fatalf("endOf in %v: %v, want %v", loc, got, want)
		}
	}
}

func TestMatchLang(t *testing.T) {
	cases := map[string]string{
		"en":               "en",
		"de":               "de",
		"de-CH":            "de", // base-tag match onto a shipped language
		"de-AT":            "de",
		"fr":               "en", // not shipped
		"pt-BR":            "en",
		"":                 "en", // malformed
		"abcdefghijklmnop": "en",
	}
	for in, want := range cases {
		if got := matchLang(in); got != want {
			t.Fatalf("matchLang(%q): %q, want %q", in, got, want)
		}
	}
}

func TestCsrfToken(t *testing.T) {
	a := csrfToken("alice")
	if a != csrfToken("alice") {
		t.Fatalf("csrfToken must be stable per identity")
	}
	if len(a) != 64 {
		t.Fatalf("csrfToken: %d hex chars, want 64 (HMAC-SHA256)", len(a))
	}
	if a == csrfToken("bob") {
		t.Fatalf("csrfToken: distinct identities must yield distinct tokens")
	}
	if csrfToken("") == a {
		t.Fatalf("csrfToken: the empty identity must yield its own token")
	}
}

func TestAbsURL(t *testing.T) {
	oldBase := cfg.BaseURL
	defer func() { cfg.BaseURL = oldBase }()

	// the public origin, when configured, is authoritative
	cfg.BaseURL = "https://meet.example.org"
	probeForm(t, "", "application/x-www-form-urlencoded", func(c fiber.Ctx) {
		if got := absURL(c, "/meet/xyz"); got != "https://meet.example.org/meet/xyz" {
			t.Errorf("absURL with base: %q", got)
		}
	})

	// without one, the request's own scheme and host stand in
	cfg.BaseURL = ""
	app := fiber.New()
	app.Post("/probe", func(c fiber.Ctx) error {
		if got := absURL(c, "/x"); got != "http://example.com/x" {
			t.Errorf("absURL fallback: %q", got)
		}
		return c.SendString("ok")
	})
	if _, err := app.Test(httptest.NewRequest(http.MethodPost, "/probe", nil)); err != nil {
		t.Fatalf("app.Test: %v", err)
	}
}

func TestIsFormPost(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"application/x-www-form-urlencoded", true},
		{"application/x-www-form-urlencoded; charset=UTF-8", true},
		{"APPLICATION/X-WWW-FORM-URLENCODED", true}, // case-insensitive
		{"application/json", false},
		{"", false},
	}
	for _, c := range cases {
		var got bool
		probeForm(t, "", c.ct, func(ctx fiber.Ctx) { got = isFormPost(ctx) })
		if got != c.want {
			t.Fatalf("isFormPost(%q): %v, want %v", c.ct, got, c.want)
		}
	}
}

// ---- the vote helpers ----

func TestValidVoteMark(t *testing.T) {
	cases := []struct {
		vt   string
		val  string
		want bool
	}{
		{"yes", "", true}, {"yes", "y", true}, {"yes", "n", false}, {"yes", "m", false},
		{"yes-no", "", true}, {"yes-no", "y", true}, {"yes-no", "n", true}, {"yes-no", "m", false},
		{"yes-maybe-no", "", true}, {"yes-maybe-no", "y", true}, {"yes-maybe-no", "n", true}, {"yes-maybe-no", "m", true},
		{"freetext", "", true}, {"freetext", "n", false},
		{"yes-no", "r", false}, {"yes-no", "g", false}, {"yes-no", "x", false},
	}
	for _, c := range cases {
		if got := validVoteMark(c.vt, c.val); got != c.want {
			t.Fatalf("validVoteMark(%q, %q): %v, want %v", c.vt, c.val, got, c.want)
		}
	}
}

func TestAssignChoiceIDs(t *testing.T) {
	old := database.ChoiceList{
		{ID: "a1", Text: "A"},
		{ID: "b2", Text: "B"},
		{ID: "c3", Text: "C"},
		{ID: "i1", Image: "img-a"},
	}
	minted := func(cs database.ChoiceList) {
		t.Helper()
		seen := make(map[string]bool, len(cs))
		for i := range cs {
			if len(cs[i].ID) != 12 {
				t.Fatalf("choice %d: ID %q is not a 12-char mint", i, cs[i].ID)
			}
			if seen[cs[i].ID] {
				t.Fatalf("duplicate ID %q", cs[i].ID)
			}
			seen[cs[i].ID] = true
		}
	}

	t.Run("reordering carries the IDs", func(t *testing.T) {
		got, err := assignChoiceIDs(old, []choiceRow{
			{text: "B", oldID: "b2"}, {text: "A", oldID: "a1"},
			{text: "C", oldID: "c3"}, {text: "", image: "img-a", oldID: "i1"},
		})
		if err != nil {
			t.Fatalf("assignChoiceIDs: %v", err)
		}
		if got[0].ID != "b2" || got[1].ID != "a1" || got[2].ID != "c3" {
			t.Fatalf("IDs not carried through the reorder: %+v", got)
		}
	})
	t.Run("a changed text is a new option", func(t *testing.T) {
		got, err := assignChoiceIDs(old, []choiceRow{
			{text: "A2", oldID: "a1"}, {text: "B", oldID: "b2"},
		})
		if err != nil {
			t.Fatalf("assignChoiceIDs: %v", err)
		}
		if got[0].ID == "a1" || len(got[0].ID) != 12 {
			t.Fatalf("a changed text must get a fresh minted ID: %+v", got)
		}
		if got[1].ID != "b2" {
			t.Fatalf("the unchanged option must keep its ID: %+v", got)
		}
	})
	t.Run("deleted rows drop, new rows mint", func(t *testing.T) {
		got, err := assignChoiceIDs(old, []choiceRow{
			{text: "B", oldID: "b2"}, {text: "D"},
		})
		if err != nil {
			t.Fatalf("assignChoiceIDs: %v", err)
		}
		if got[0].ID != "b2" {
			t.Fatalf("kept ID lost: %+v", got)
		}
		if len(got[1].ID) != 12 {
			t.Fatalf("the new row must get a fresh minted ID: %+v", got)
		}
		for _, want := range []string{"c3", "a1", "b2"} {
			if got[1].ID == want {
				t.Fatalf("the new row's ID collides with a stored one: %+v", got)
			}
		}
	})
	t.Run("a claimed ID twice keeps it for the first row only", func(t *testing.T) {
		got, err := assignChoiceIDs(old, []choiceRow{
			{text: "B", oldID: "b2"}, {text: "B", oldID: "b2"},
		})
		if err != nil {
			t.Fatalf("assignChoiceIDs: %v", err)
		}
		if got[0].ID != "b2" || got[1].ID == "b2" {
			t.Fatalf("the duplicate claim must mint a fresh ID: %+v", got)
		}
	})
	t.Run("an unknown oldID mints fresh", func(t *testing.T) {
		got, err := assignChoiceIDs(old, []choiceRow{{text: "B", oldID: "nope"}})
		if err != nil {
			t.Fatalf("assignChoiceIDs: %v", err)
		}
		if got[0].ID == "nope" {
			t.Fatalf("an ID the stored vote never had must not be trusted: %+v", got)
		}
	})
	t.Run("a swapped image is a new option", func(t *testing.T) {
		got, err := assignChoiceIDs(old, []choiceRow{
			{text: "", image: "img-b", oldID: "i1"}, // the label-less option's picture swapped
			{text: "", image: "img-a", oldID: "i1"}, // the original pair, claimed again
		})
		if err != nil {
			t.Fatalf("assignChoiceIDs: %v", err)
		}
		if got[0].ID == "i1" {
			t.Fatalf("a swapped image must get a fresh minted ID: %+v", got)
		}
		if got[1].ID != "i1" || got[1].Image != "img-a" {
			t.Fatalf("the unchanged (label, image) pair must keep its ID: %+v", got)
		}
	})
	t.Run("create mints every ID (text rows and image-only rows)", func(t *testing.T) {
		got, err := assignChoiceIDs(nil, []choiceRow{
			{text: "A"}, {text: "", image: "img-x"},
		})
		if err != nil {
			t.Fatalf("assignChoiceIDs: %v", err)
		}
		minted(got)
		if got[1].Image != "img-x" {
			t.Fatalf("the image ID must ride along with the row: %+v", got)
		}
	})
}

func TestVoteChoiceImageIDs(t *testing.T) {
	if ids := voteChoiceImageIDs(nil); ids != nil {
		t.Fatalf("a nil vote contributes no image IDs: %+v", ids)
	}
	v := &database.Vote{Choices: database.ChoiceList{
		{ID: "a", Image: "img-1"},
		{ID: "b", Text: "no image"},
		{ID: "c", Image: "img-1"}, // the same image twice
		{ID: "d", Image: "img-2"},
	}}
	ids := voteChoiceImageIDs(v)
	if len(ids) != 2 || ids[0] != "img-1" || ids[1] != "img-2" {
		t.Fatalf("distinct image IDs in choice order, want [img-1 img-2], got %+v", ids)
	}
}

func TestAnyRowHasImage(t *testing.T) {
	if anyRowHasImage(nil) {
		t.Fatal("no rows, no image")
	}
	if anyRowHasImage([]choiceRow{{text: "A"}, {text: "B"}}) {
		t.Fatal("text-only rows carry no image")
	}
	if !anyRowHasImage([]choiceRow{{text: "A"}, {image: "img"}}) {
		t.Fatal("an image row must count")
	}
}

func TestDiffImageIDs(t *testing.T) {
	old := database.ChoiceList{
		{ID: "a", Image: "img-1"},
		{ID: "b", Image: "img-2"},
		{ID: "c", Image: "img-2"}, // duplicated in the old list
		{ID: "d", Text: "plain"},
	}
	newList := database.ChoiceList{
		{ID: "a", Image: "img-1"},
		{ID: "e", Image: "img-3"},
	}
	dropped := diffImageIDs(old, newList)
	if len(dropped) != 1 || dropped[0] != "img-2" {
		t.Fatalf("the only dropped image is img-2, got %+v", dropped)
	}
	if got := diffImageIDs(old, old); len(got) != 0 {
		t.Fatalf("no change drops nothing: %+v", got)
	}
	if got := diffImageIDs(nil, nil); got != nil {
		t.Fatalf("no images drop nothing: %+v", got)
	}
}

func TestPruneVoteAnswers(t *testing.T) {
	t.Run("marks and notes for removed options are dropped", func(t *testing.T) {
		v := &database.Vote{
			Choices:  database.ChoiceList{{ID: "a1", Text: "A"}},
			VoteType: database.VoteTypeYesNo,
			Answers: database.VoteAnswerList{{
				Marks: map[string]string{"a1": "y", "b2": "y"},
				Notes: map[string]string{"a1": "n1", "b2": "n2"},
			}},
		}
		pruneVoteAnswers(v)
		if len(v.Answers[0].Marks) != 1 || v.Answers[0].Marks["a1"] != "y" {
			t.Fatalf("marks not pruned: %+v", v.Answers[0].Marks)
		}
		if v.Answers[0].Notes != nil {
			t.Fatalf("a mark-mode vote keeps no notes: %+v", v.Answers[0].Notes)
		}
	})
	choices3 := database.ChoiceList{{ID: "a1"}, {ID: "a2"}, {ID: "a3"}}
	t.Run("flipping to yes drops the n and m marks", func(t *testing.T) {
		v := &database.Vote{
			Choices:  choices3,
			VoteType: database.VoteTypeYes,
			Answers:  database.VoteAnswerList{{Marks: map[string]string{"a1": "y", "a2": "n", "a3": "m"}}},
		}
		pruneVoteAnswers(v)
		if len(v.Answers[0].Marks) != 1 || v.Answers[0].Marks["a1"] != "y" {
			t.Fatalf("n/m marks not pruned from a yes vote: %+v", v.Answers[0].Marks)
		}
	})
	t.Run("flipping to yes-no drops the m marks", func(t *testing.T) {
		v := &database.Vote{
			Choices:  choices3,
			VoteType: database.VoteTypeYesNo,
			Answers:  database.VoteAnswerList{{Marks: map[string]string{"a1": "y", "a2": "n", "a3": "m"}}},
		}
		pruneVoteAnswers(v)
		if len(v.Answers[0].Marks) != 2 || v.Answers[0].Marks["a2"] != "n" {
			t.Fatalf("m mark not pruned from a yes-no vote: %+v", v.Answers[0].Marks)
		}
	})
	t.Run("flipping to free text drops the marks, keeps the notes", func(t *testing.T) {
		v := &database.Vote{
			Choices:  choices3,
			VoteType: database.VoteTypeFreeText,
			Answers:  database.VoteAnswerList{{Marks: map[string]string{"a1": "y"}, Notes: map[string]string{"a1": "keep"}}},
		}
		pruneVoteAnswers(v)
		if v.Answers[0].Marks != nil {
			t.Fatalf("a free-text vote keeps no marks: %+v", v.Answers[0].Marks)
		}
		if v.Answers[0].Notes["a1"] != "keep" {
			t.Fatalf("notes lost: %+v", v.Answers[0].Notes)
		}
	})
}

func TestCollectMarks(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatalf("loading zone: %v", err)
	}
	slot1 := slotKey(time.Date(2026, 11, 1, 10, 0, 0, 0, berlin))
	slot2 := slotKey(time.Date(2026, 11, 1, 14, 0, 0, 0, berlin))
	m := &database.Meeting{Dates: database.TimeSlice{time.Date(2026, 11, 1, 10, 0, 0, 0, berlin), time.Date(2026, 11, 1, 14, 0, 0, 0, berlin)}}
	single := &database.Meeting{Dates: m.Dates, SingleSignup: true}

	probe := func(mt *database.Meeting, form string) (map[string]string, string) {
		var marks map[string]string
		var msg string
		probeForm(t, form, "application/x-www-form-urlencoded", func(c fiber.Ctx) {
			marks, msg = collectMarks(c, mt)
		})
		return marks, msg
	}

	marks, msg := probe(m, "a"+slot1+"=g&a"+slot2+"=y")
	if msg != "" || marks[slot1] != "g" || marks[slot2] != "y" || len(marks) != 2 {
		t.Fatalf("two marked slots: %+v, %q", marks, msg)
	}
	marks, msg = probe(m, "")
	if msg != "" || len(marks) != 0 {
		t.Fatalf("no marks: %+v, %q", marks, msg)
	}
	if _, msg = probe(m, "a"+slot1+"=r"); msg != "Invalid answer option" {
		t.Fatalf("a red mark is never submitted, %q", msg)
	}

	if marks, msg := probe(single, "a"+slot1+"=y"); msg != "This poll allows signing up for only one time slot" {
		t.Fatalf("single-signup with a maybe: %q (marks %+v)", msg, marks)
	}
	if marks, msg := probe(single, "a"+slot1+"=g&a"+slot2+"=g"); msg != "This poll allows signing up for only one time slot" {
		t.Fatalf("single-signup with two greens: %q (marks %+v)", msg, marks)
	}
	marks, msg = probe(single, "a"+slot1+"=g")
	if msg != "" || len(marks) != 1 {
		t.Fatalf("single-signup with one green: %+v, %q", marks, msg)
	}
}

func TestCollectVoteAnswers(t *testing.T) {
	v := &database.Vote{
		VoteType: database.VoteTypeYesNo,
		Choices:  database.ChoiceList{{ID: "a1", Text: "A"}, {ID: "a2", Text: "B"}},
	}
	must := &database.Vote{VoteType: v.VoteType, Choices: v.Choices, MustAnswer: true}
	single := &database.Vote{VoteType: v.VoteType, Choices: v.Choices, SingleChoice: true}
	ft := &database.Vote{VoteType: database.VoteTypeFreeText, Choices: v.Choices}
	ftSingle := &database.Vote{VoteType: ft.VoteType, Choices: ft.Choices, SingleChoice: true}
	ftMust := &database.Vote{VoteType: ft.VoteType, Choices: ft.Choices, MustAnswer: true}

	probe := func(vt *database.Vote, form string) (map[string]string, map[string]string, string) {
		var marks, notes map[string]string
		var msg string
		probeForm(t, form, "application/x-www-form-urlencoded", func(c fiber.Ctx) {
			marks, notes, msg = collectVoteAnswers(c, vt)
		})
		return marks, notes, msg
	}

	marks, notes, msg := probe(v, "aa1=y&aa2=n")
	if msg != "" || notes != nil || marks["a1"] != "y" || marks["a2"] != "n" {
		t.Fatalf("yes/no marks: %+v, %+v, %q", marks, notes, msg)
	}
	marks, notes, msg = probe(v, "")
	if msg != "" || len(marks) != 0 {
		t.Fatalf("neutral (unmarked) options: %+v, %q", marks, msg)
	}
	if _, _, msg = probe(v, "aa1=m"); msg != "Invalid answer option" {
		t.Fatalf("a maybe in a yes/no vote: %q", msg)
	}
	if _, _, msg = probe(must, "aa1=y"); msg != "Please answer every option" {
		t.Fatalf("must-answer with one unanswered option: %q", msg)
	}
	if _, _, msg = probe(single, "aa1=y&aa2=y"); msg != "This vote allows choosing only one option" {
		t.Fatalf("single-choice with two yes: %q", msg)
	}

	_, notes, msg = probe(ft, "na1=has food")
	if msg != "" || notes["a1"] != "has food" {
		t.Fatalf("free-text note: %+v, %q", notes, msg)
	}
	if _, _, msg = probe(ftSingle, "na1=a&na2=b"); msg != "This vote allows choosing only one option" {
		t.Fatalf("single-choice with two notes: %q", msg)
	}
	if _, _, msg = probe(ftMust, "na1=a"); msg != "Please answer every option" {
		t.Fatalf("must-answer with one empty note: %q", msg)
	}
	if _, _, msg = probe(ft, "na1="+strings.Repeat("x", maxVoteNoteLen+1)); msg != "Note too long (max 150 characters)" {
		t.Fatalf("an overlong note: %q", msg)
	}
}

func TestZoneLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Etc/GMT-5", "UTC+5"}, // the IANA sign is inverted
		{"Etc/GMT+5", "UTC-5"},
		{"Etc/GMT+12", "UTC-12"},
		{"Etc/GMT-14", "UTC+14"},
		{"Etc/GMT", "UTC"},
		{"UTC", "UTC"},
		{"America/New_York", "America/New York"},
		{"Europe/Berlin", "Europe/Berlin"},
	}
	for _, c := range cases {
		if got := zoneLabel(c.in); got != c.want {
			t.Fatalf("zoneLabel(%q): %q, want %q", c.in, got, c.want)
		}
	}
}

func TestOffsetLabel(t *testing.T) {
	cases := []struct {
		in   int
		want string
	}{
		{0, "UTC"},
		{3600, "UTC+1"},
		{-18000, "UTC-5"},
		{19800, "UTC+5:30"},
		{20700, "UTC+5:45"},
		{-43200, "UTC-12"},
	}
	for _, c := range cases {
		if got := offsetLabel(c.in); got != c.want {
			t.Fatalf("offsetLabel(%d): %q, want %q", c.in, got, c.want)
		}
	}
}

func TestZoneOptionLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"UTC", "UTC"}, // bare UTC stands alone
		{"Europe/Berlin", "Europe/Berlin (UTC+1)"}, // standard offset, not the summer one
		{"America/New_York", "America/New York (UTC-5)"},
		{"Europe/London", "Europe/London (UTC)"},    // zero standard offset
		{"Asia/Kolkata", "Asia/Kolkata (UTC+5:30)"}, // fractional offset
		{"Asia/Kathmandu", "Asia/Kathmandu (UTC+5:45)"},
		{"Not/AZone", "Not/AZone"}, // unloadable: no offset
	}
	for _, c := range cases {
		if got := zoneOptionLabel(c.in); got != c.want {
			t.Fatalf("zoneOptionLabel(%q): %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToggleFinalSlot(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	d1 := time.Date(2026, 11, 1, 10, 0, 0, 0, berlin)
	d2 := time.Date(2026, 11, 1, 14, 0, 0, 0, berlin)
	dates := database.TimeSlice{d1, d2}

	// Adding a slot appends it.
	out := toggleFinalSlot(dates, nil, slotKey(d1))
	if len(out) != 1 || slotKey(out[0]) != slotKey(d1) {
		t.Fatalf("toggleFinalSlot add: %+v, want [%v]", out, slotKey(d1))
	}
	// Adding a second keeps both.
	out = toggleFinalSlot(dates, out, slotKey(d2))
	if len(out) != 2 {
		t.Fatalf("toggleFinalSlot add second: %+v, want 2", out)
	}
	// Toggling an existing slot removes it.
	out = toggleFinalSlot(dates, out, slotKey(d1))
	if len(out) != 1 || slotKey(out[0]) != slotKey(d2) {
		t.Fatalf("toggleFinalSlot remove: %+v, want [%v]", out, slotKey(d2))
	}
	// A slot the poll doesn't offer is a no-op.
	if got := toggleFinalSlot(dates, out, "2020-01-01T00:00Z"); len(got) != 1 {
		t.Fatalf("toggleFinalSlot unknown slot must not add: %+v", got)
	}
}

func TestToggleFinalChoice(t *testing.T) {
	// Add, add-second, remove, unknown-id no-op.
	out := toggleFinalChoice(nil, "c1")
	if len(out) != 1 || out[0] != "c1" {
		t.Fatalf("toggleFinalChoice add: %+v, want [c1]", out)
	}
	out = toggleFinalChoice(out, "c2")
	if len(out) != 2 {
		t.Fatalf("toggleFinalChoice add second: %+v, want 2", out)
	}
	out = toggleFinalChoice(out, "c1")
	if len(out) != 1 || out[0] != "c2" {
		t.Fatalf("toggleFinalChoice remove: %+v, want [c2]", out)
	}
	// The helper trusts its caller (the handler checks the id against the
	// vote's options), so any id is appended.
	out = toggleFinalChoice(out, "c3")
	if len(out) != 2 || out[1] != "c3" {
		t.Fatalf("toggleFinalChoice append: %+v, want c3 last", out)
	}
}

func TestPruneFinalSlots(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	d1 := time.Date(2026, 11, 1, 10, 0, 0, 0, berlin)
	d2 := time.Date(2026, 11, 2, 10, 0, 0, 0, berlin)
	m := &database.Meeting{
		Dates:      database.TimeSlice{d1},
		FinalSlots: database.TimeSlice{d1, d2},
	}
	pruneFinalSlots(m)
	if len(m.FinalSlots) != 1 || slotKey(m.FinalSlots[0]) != slotKey(d1) {
		t.Fatalf("pruneFinalSlots: %+v, want only the surviving slot", m.FinalSlots)
	}
	// An empty list stays empty.
	m2 := &database.Meeting{Dates: database.TimeSlice{d1}}
	pruneFinalSlots(m2)
	if m2.FinalSlots != nil {
		t.Fatalf("pruneFinalSlots empty: %+v, want nil", m2.FinalSlots)
	}
}

func TestPruneFinalChoices(t *testing.T) {
	v := &database.Vote{
		Choices:      database.ChoiceList{{ID: "c1"}, {ID: "c2"}},
		FinalChoices: database.StringSlice{"c1", "c3"},
	}
	pruneFinalChoices(v)
	if len(v.FinalChoices) != 1 || v.FinalChoices[0] != "c1" {
		t.Fatalf("pruneFinalChoices: %+v, want only the surviving option", v.FinalChoices)
	}
	v2 := &database.Vote{Choices: database.ChoiceList{{ID: "c1"}}}
	pruneFinalChoices(v2)
	if v2.FinalChoices != nil {
		t.Fatalf("pruneFinalChoices empty: %+v, want nil", v2.FinalChoices)
	}
}
