package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/store"
)

// staticDir is the absolute path of the template dir, resolved once before
// any test chdirs into its temp dir.
var staticDir string

func TestMain(m *testing.M) {
	abs, err := filepath.Abs("static")
	if err != nil {
		fmt.Fprintln(os.Stderr, "resolving static dir:", err)
		os.Exit(1)
	}
	staticDir = abs
	os.Exit(m.Run())
}

// withStore opens a throwaway storage backend of the given kind under a
// fresh temp working directory and wires it into the package-level db, so
// the suites never touch the dev data files.
func withStore(t *testing.T, kind, dsn string) {
	t.Helper()

	t.Chdir(t.TempDir())
	st, err := store.Open(kind, dsn)
	if err != nil {
		t.Fatalf("opening %q store: %v", kind, err)
	}
	db = st
	t.Cleanup(func() { _ = st.Close() })
}

func newTestApp(t *testing.T) *fiber.App {
	t.Helper()
	return newApp(staticDir)
}

// postForm submits a url-encoded body to /1-1-form, like a browser does.
func postForm(t *testing.T, app *fiber.App, form string) (*http.Response, error) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/1-1-form", strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return app.Test(req)
}

// runSmokeSuite walks the request lifecycle in-process (no network): page
// rendering, static serving, the 404 fallthrough, and a full meeting
// create -> redirect -> read round trip.
func runSmokeSuite(t *testing.T) {
	app := newTestApp(t)

	body := func(r *http.Response) string {
		t.Helper()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading body: %v", err)
		}
		return string(b)
	}

	get := func(path string) *http.Response {
		t.Helper()
		r, err := app.Test(httptest.NewRequest(http.MethodGet, path, nil))
		if err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
		return r
	}

	// Pages render through the template engine.
	if r := get("/"); r.StatusCode != http.StatusOK || !strings.Contains(body(r), "kiusan") {
		t.Fatalf("GET /: status %d, body missing page content", r.StatusCode)
	}
	// The timezone select is rendered from data/timezones.txt (curated:
	// 1-2 zones per UTC offset per region, plus UTC). The assertion is
	// structural — the options must be inside the <select>, not loose text.
	rf := get("/1-1-form")
	if rf.StatusCode != http.StatusOK {
		t.Fatalf("GET /1-1-form: status %d", rf.StatusCode)
	}
	fb := body(rf)
	selStart := strings.Index(fb, `<select id="timezone" name="timezone">`)
	selEnd := strings.Index(fb, "</select>")
	if selStart < 0 || selEnd <= selStart {
		t.Fatalf("GET /1-1-form: timezone <select> not found in body")
	}
	sel := fb[selStart:selEnd]
	for _, want := range []string{
		`<option value="Europe/Berlin">Europe/Berlin</option>`,
		`<option value="America/New_York">America/New York</option>`,
		`<option value="UTC">UTC</option>`,
	} {
		if !strings.Contains(sel, want) {
			t.Fatalf("GET /1-1-form: timezone select missing %s", want)
		}
	}
	// The options render in alphabetical order.
	var names []string
	for _, o := range strings.Split(sel, `<option value="`)[1:] {
		names = append(names, o[:strings.Index(o, `">`)])
	}
	if len(names) == 0 {
		t.Fatalf("GET /1-1-form: no timezone options rendered")
	}
	for k := 1; k < len(names); k++ {
		if names[k-1] > names[k] {
			t.Fatalf("GET /1-1-form: timezone options not alphabetical: %q before %q", names[k-1], names[k])
		}
	}

	// The SVG favicon, css and the self-hosted font from the static dir.
	if r := get("/logo.svg"); r.StatusCode != http.StatusOK {
		t.Fatalf("GET /logo.svg: status %d", r.StatusCode)
	}
	if r := get("/style.css"); r.StatusCode != http.StatusOK {
		t.Fatalf("GET /style.css: status %d", r.StatusCode)
	}
	fr := get("/InterVariable.woff2")
	if fr.StatusCode != http.StatusOK {
		t.Fatalf("GET /InterVariable.woff2: status %d", fr.StatusCode)
	}
	if fb := body(fr); len(fb) < 4 || fb[:4] != "wOF2" {
		t.Fatalf("GET /InterVariable.woff2: not a valid woff2 (%d bytes)", len(fb))
	}

	// Unknown paths (and unknown meetings) fall through the statics to the 404 handler.
	for _, p := range []string{"/definitely-missing", "/meet/not-created-by-anyone"} {
		if r := get(p); r.StatusCode != http.StatusNotFound {
			t.Fatalf("GET %s: expected 404, got %d", p, r.StatusCode)
		}
	}

	// Create a meeting via the form and read it back through the redirect.
	r, err := postForm(t, app, "title=Smoke+Meeting&email=smoke%40example.com&description=upgrade+check&timezone=Europe/Berlin&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00&slot2=2026-11-02T10:00&slot3=2026-11-02T14:00")
	if err != nil {
		t.Fatalf("POST /1-1-form: %v", err)
	}
	if r.StatusCode != 303 {
		t.Fatalf("POST /1-1-form: expected 303, got %d", r.StatusCode)
	}
	loc := r.Header.Get("Location")
	if !strings.HasPrefix(loc, "/meet/") || len(loc) <= len("/meet/") {
		t.Fatalf("POST /1-1-form: unexpected Location %q", loc)
	}

	mr := get(loc)
	if mr.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", loc, mr.StatusCode)
	}
	mb := body(mr)
	for _, want := range []string{
		"Smoke Meeting",
		"smoke@example.com",
		"Timezone: Europe/Berlin",
		`class="th-year" colspan="4"`,
		`class="th-date" colspan="2"`,
		`title="Sun 1 Nov 2026"`, "1 Nov\nSun",
		`title="Mon 2 Nov 2026"`, "2 Nov\nMon",
		`class="th-time"`, "10:00", "14:00",
	} {
		if !strings.Contains(mb, want) {
			t.Fatalf("GET %s: expected %q in body, got:\n%s", loc, want, mb)
		}
	}

	// The time sub-row's cells must sit inside their own <tr>: a stray
	// </tr> would orphan them and the browser would reflow the header.
	if regexp.MustCompile(`</tr>\s*<th class="th-time"`).MatchString(mb) {
		t.Fatalf("GET %s: time header cells orphaned from their <tr>", loc)
	}
}

// runFormPostSuite pins the form contract: which submissions create a
// meeting (303) and which the handler rejects (400), including the
// month/day layout trap.
func runFormPostSuite(t *testing.T) {
	app := newTestApp(t)

	tests := []struct {
		name       string
		form       string
		wantStatus int
		// when set, must appear in the response body
		wantBody string
	}{
		{"single date", "title=A&email=a%40b.c&description=d&slot0=2026-11-01", 303, ""},
		{"two dates", "title=A&email=a%40b.c&description=d&slot0=2026-11-01&slot1=2026-11-02", 303, ""},
		{"no dates (accepted today; pinned as current behavior)", "title=A&email=a%40b.c&description=d", 303, ""},
		{"swapped month/day", "title=A&email=a%40b.c&description=d&slot0=01-11-2026", 400, "Invalid date"},
		{"two dates with two times", "title=A&email=a%40b.c&description=d&slot0=2026-11-01T10:00&slot1=2026-11-01T14:00&slot2=2026-11-02T10:00&slot3=2026-11-02T14:00", 303, ""},
		{"per-date times", "title=A&email=a%40b.c&description=d&slot0=2026-11-01T10:00&slot1=2026-11-02T14:00", 303, ""},
		{"bad time", "title=A&email=a%40b.c&description=d&slot0=2026-11-01T99:99", 400, "Invalid time"},
		{"bad timezone", "title=A&email=a%40b.c&description=d&slot0=2026-11-01&timezone=Not/AZone", 400, "Invalid timezone"},
		{"impossible day", "title=A&email=a%40b.c&description=d&slot0=2026-02-30", 400, "Invalid date"},
		{"malformed encoding", "title=A&email=a%40b.c&description=d&slot0=%E0%41", 400, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := postForm(t, app, tt.form)
			if err != nil {
				t.Fatalf("POST /1-1-form: %v", err)
			}
			if r.StatusCode != tt.wantStatus {
				t.Fatalf("expected %d, got %d", tt.wantStatus, r.StatusCode)
			}
			if tt.wantBody != "" {
				b, _ := io.ReadAll(r.Body)
				if !strings.Contains(string(b), tt.wantBody) {
					t.Fatalf("expected %q in body, got %q", tt.wantBody, string(b))
				}
			}
		})
	}
}

// TestHeaderLabelConsistency pins the date header's label format: every
// date renders with as many lines as the narrowest date column allows,
// so a poll mixing single-slot and multi-slot dates stays readable.
func TestHeaderLabelConsistency(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	app := newTestApp(t)

	r, err := postForm(t, app, "title=Mixed&email=m%40example.com&description=d&timezone=Europe/Berlin&slot0=2026-11-01T10:00&slot1=2026-11-02T10:00&slot2=2026-11-02T14:00")
	if err != nil {
		t.Fatalf("POST /1-1-form: %v", err)
	}
	if r.StatusCode != 303 {
		t.Fatalf("POST /1-1-form: expected 303, got %d", r.StatusCode)
	}
	mr, err := app.Test(httptest.NewRequest(http.MethodGet, r.Header.Get("Location"), nil))
	if err != nil {
		t.Fatalf("GET meeting: %v", err)
	}
	b, _ := io.ReadAll(mr.Body)
	mb := string(b)
	// The single-slot 1 Nov (span 1) forces the three-line format on the
	// two-slot 2 Nov as well.
	for _, want := range []string{
		`class="th-date" colspan="1"`, `class="th-date" colspan="2"`,
		"1\nNov\nSun", "2\nNov\nMon",
	} {
		if !strings.Contains(mb, want) {
			t.Fatalf("GET meeting: expected %q in body, got:\n%s", want, mb)
		}
	}
}

// The smoke and form-post suites run on GORM-backed sqlite (the default,
// cgo-free), covering the storage end to end.

func TestSmokeSqlite(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	runSmokeSuite(t)
}

func TestFormPostSqlite(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	runFormPostSuite(t)
}

// TestRandomKey checks the key contract: exact length, URL-safe charset,
// and no collisions across samples.
func TestRandomKey(t *testing.T) {
	const samples = 200
	seen := make(map[string]struct{}, samples)
	for i := 0; i < samples; i++ {
		key, err := store.RandomKey(16)
		if err != nil {
			t.Fatalf("store.RandomKey(16): %v", err)
		}
		if len(key) != 16 {
			t.Fatalf("key length %d, want 16: %q", len(key), key)
		}
		for j, c := range key {
			if !strings.ContainsRune(store.URLSafeLetters, c) {
				t.Fatalf("byte %d of %q (%q) not in URLSafeLetters", j, key, c)
			}
		}
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate key generated: %q", key)
		}
		seen[key] = struct{}{}
	}
	for _, n := range []int{1, 5, 64, 256} {
		key, err := store.RandomKey(n)
		if err != nil {
			t.Fatalf("store.RandomKey(%d): %v", n, err)
		}
		if l := len(key); l != n {
			t.Fatalf("store.RandomKey(%d) returned length %d", n, l)
		}
	}
}
