package main

import (
	"encoding/json"
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

// i18nGet sends a GET with an optional locale cookie and Accept-Language
// header ("" for neither) and returns the response.
func i18nGet(t *testing.T, app *fiber.App, path, cookie, acceptLanguage string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if acceptLanguage != "" {
		req.Header.Set("Accept-Language", acceptLanguage)
	}
	r, err := app.Test(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return r
}

func i18nBody(t *testing.T, r *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return string(b)
}

// TestI18nResolution pins the locale resolution order (cookie →
// Accept-Language → English), the base-language matching, the fallback
// for unshipped languages, and the dropdown's selected state.
func TestI18nResolution(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	app := newTestApp(t)

	check := func(path, cookie, acceptLang, lang, marker string) {
		t.Helper()
		b := i18nBody(t, i18nGet(t, app, path, cookie, acceptLang))
		if !strings.Contains(b, `lang="`+lang+`"`) {
			t.Fatalf("GET %s (cookie %q, accept %q): html lang, want %q in:\n%s",
				path, cookie, acceptLang, lang, b)
		}
		if marker != "" && !strings.Contains(b, marker) {
			t.Fatalf("GET %s (cookie %q, accept %q): missing %q",
				path, cookie, acceptLang, marker)
		}
	}

	// No cookie, no header: English.
	check("/", "", "", "en", `>Skip to content<`)
	// The cookie wins over the header.
	check("/", "kiusan-locale=de", "fr-FR", "de", `>Zum Inhalt springen<`)
	// Without a cookie, Accept-Language decides (de-DE → de).
	check("/", "", "de-DE,de;q=0.9,en;q=0.8", "de", `>Zum Inhalt springen<`)
	// An unshipped language falls back to English.
	check("/", "", "fr-FR", "en", `>Skip to content<`)
	// A malformed cookie is ignored.
	check("/", "kiusan-locale=%22%3E%22", "fr-FR", "en", `>Skip to content<`)
	// A region variant of a shipped language resolves to it.
	check("/", "kiusan-locale=de-CH", "", "de", `>Zum Inhalt springen<`)

	// The dropdown marks the cookie's language selected — and only it.
	b := i18nBody(t, i18nGet(t, app, "/", "kiusan-locale=de", ""))
	if !strings.Contains(b, `<option value="de" selected>Deutsch</option>`) {
		t.Fatalf("dropdown: de option not marked selected:\n%s", b)
	}
	if strings.Contains(b, `<option value="" selected>`) {
		t.Fatalf("dropdown: browser-default option must not be selected with a cookie:\n%s", b)
	}
	b = i18nBody(t, i18nGet(t, app, "/", "", ""))
	if !strings.Contains(b, `<option value="" selected>Browser default</option>`) {
		t.Fatalf("dropdown: browser-default option not marked selected without a cookie:\n%s", b)
	}

	// The meeting and form pages translate too.
	m, adminKey := newPollClient(t, app).createPoll(
		"title=I18n+Poll&email=i18n%40example.com&description=d&timezone=Europe/Berlin&slot0=2026-11-01T10:00")
	b = i18nBody(t, i18nGet(t, app, "/meet/"+adminKey, "kiusan-locale=de", ""))
	for _, want := range []string{
		`lang="de"`, "Teilnehmerlink (diesen teilen):",
		"Adminlink (diesen für dich behalten):", "Abstimmung bearbeiten",
	} {
		if !strings.Contains(b, want) {
			t.Fatalf("admin page (de) missing %q", want)
		}
	}
	b = i18nBody(t, i18nGet(t, app, "/meet-form/edit/"+adminKey, "kiusan-locale=de", ""))
	for _, want := range []string{
		`lang="de"`, "Abstimmung bearbeiten", `>Titel:<`,
		`<title data-i18n-prefix="pagetitle.formEdit" data-suffix="I18n Poll">kiusan | Bearbeiten: I18n Poll</title>`,
	} {
		if !strings.Contains(b, want) {
			t.Fatalf("edit form (de) missing %q", want)
		}
	}
	_ = m
}

// TestI18nEndpoints pins the /i18n contract: the boot script, the merged
// per-language dictionaries (target over the English fallback), the
// English fallback for unshipped tags, and the 404 for malformed tags.
func TestI18nEndpoints(t *testing.T) {
	withStore(t, store.BackendSQLite, "meetings.db")
	app := newTestApp(t)

	// /i18n/boot serves the resolved language's dictionary as a script.
	r := i18nGet(t, app, "/i18n/boot", "kiusan-locale=de", "")
	if r.StatusCode != http.StatusOK {
		t.Fatalf("GET /i18n/boot: status %d", r.StatusCode)
	}
	if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("GET /i18n/boot: Content-Type %q", ct)
	}
	b := i18nBody(t, r)
	if !strings.HasPrefix(b, "window.I18N = ") || !strings.Contains(b, `"Zeitzone:"`) {
		t.Fatalf("GET /i18n/boot: unexpected body:\n%s", b)
	}

	dict := func(tag string) *i18nDict {
		t.Helper()
		r := i18nGet(t, app, "/i18n/"+tag, "", "")
		if r.StatusCode != http.StatusOK {
			t.Fatalf("GET /i18n/%s: status %d", tag, r.StatusCode)
		}
		var d i18nDict
		if err := json.Unmarshal([]byte(i18nBody(t, r)), &d); err != nil {
			t.Fatalf("GET /i18n/%s: bad JSON: %v", tag, err)
		}
		return &d
	}

	// The de dictionary is complete (every en key) and overridden.
	d := dict("de")
	if d.Lang != "de" {
		t.Fatalf("de dict: lang %q", d.Lang)
	}
	if d.T["form.timezone"] != "Zeitzone:" {
		t.Fatalf("de dict: form.timezone %q", d.T["form.timezone"])
	}
	if len(d.T) != len(readCatalogFile("en")) {
		t.Fatalf("de dict: %d keys, want the full en key set (%d)",
			len(d.T), len(readCatalogFile("en")))
	}

	// An unshipped (or region-variant) tag resolves onto a shipped one.
	if d = dict("fr-FR"); d.Lang != "en" {
		t.Fatalf("fr-FR dict: lang %q, want the en fallback", d.Lang)
	}
	if d = dict("de-DE"); d.Lang != "de" {
		t.Fatalf("de-DE dict: lang %q, want de", d.Lang)
	}

	// A malformed tag 404s.
	if r := i18nGet(t, app, "/i18n/%22bad%22", "", ""); r.StatusCode != http.StatusNotFound {
		t.Fatalf("GET /i18n/\"bad\": status %d, want 404", r.StatusCode)
	}
}

// TestI18nKeyParity pins that every non-base catalog only overrides keys
// the English base defines — a typo'd key would render as a bare key.
func TestI18nKeyParity(t *testing.T) {
	en := readCatalogFile("en")
	for _, tag := range i18nLanguages {
		s := tag.String()
		if s == "en" {
			continue
		}
		for k := range readCatalogFile(s) {
			if _, ok := en[k]; !ok {
				t.Fatalf("%s: key %q missing from the en base catalog", s, k)
			}
		}
	}
}

// TestI18nMarkerSync pins that every data-i18n* marker in the templates
// names a key the English catalog defines — the server render and the
// client swapper share one key space, and drift in either direction
// breaks a language.
func TestI18nMarkerSync(t *testing.T) {
	en := readCatalogFile("en")
	re := regexp.MustCompile(`data-i18n(?:-ph|-title|-aria|-meta|-prefix)?="([^"]+)"`)
	err := filepath.WalkDir(staticDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
			// Markers may carry a template action (the key is chosen per
			// request state); only literal values are checkable here.
			if strings.HasPrefix(m[1], "{{") {
				continue
			}
			if _, ok := en[m[1]]; !ok {
				t.Errorf("%s: marker %q names no key in the en catalog", path, m[1])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking templates: %v", err)
	}
}
