package main

import (
	"embed"
	"encoding/json"
	"log"
	"strings"

	contribi18n "github.com/gofiber/contrib/v3/i18n"
	"github.com/gofiber/fiber/v3"
	"golang.org/x/text/language"
	"gopkg.in/yaml.v2"
)

//go:embed locales
var i18nFS embed.FS

// i18nLanguages are the shipped UI languages, in dropdown order. Any
// other BCP-47 tag (de-CH, pt-BR, ...) resolves onto one of these through
// matchLang — the dropdown only offers languages a catalog exists for.
var i18nLanguages = []language.Tag{language.English, language.German}

// i18nDisplayNames are the native names the dropdown options show.
var i18nDisplayNames = map[string]string{
	"en": "English",
	"de": "Deutsch",
}

// localeOption is one row of the server-rendered language dropdown.
type localeOption struct {
	Tag  string
	Name string
}

// localeOptions is the dropdown list, built once from the shipped
// languages (the client no longer carries its own locale list).
var localeOptions = func() []localeOption {
	opts := make([]localeOption, 0, len(i18nLanguages))
	for _, t := range i18nLanguages {
		s := t.String()
		opts = append(opts, localeOption{Tag: s, Name: i18nDisplayNames[s]})
	}
	return opts
}()

// translator renders the pages in the viewer's language (gofiber contrib
// i18n over the YAML catalogs in locales/). Built once at process start:
// a missing or malformed catalog file panics here, so a broken
// translation takes the app down loudly instead of serving a
// half-localized page.
var translator = contribi18n.New(&contribi18n.Config{
	RootPath:        "locales",
	AcceptLanguages: i18nLanguages,
	DefaultLanguage: language.English,
	Loader:          &contribi18n.EmbedLoader{FS: i18nFS},
	LangHandler:     langHandler,
})

// localeCookieName is the unpersonalized display-preference cookie the
// dropdown writes (JS-side, 1-year Lax) — the same cookie the date
// display already reads.
const localeCookieName = "kiusan-locale"

// langHandler resolves the request's language: the kiusan-locale cookie
// (the viewer's explicit choice) wins, then the Accept-Language header
// (the browser default), then English. The result is always one of the
// shipped tags — the translator's localizer map is keyed by those exact
// strings.
func langHandler(c fiber.Ctx, defaultLang string) string {
	if v := c.Cookies(localeCookieName); v != "" {
		return matchLang(v)
	}
	for _, part := range strings.Split(c.Get("Accept-Language"), ",") {
		// "de-DE,de;q=0.9,en;q=0.8": the first listed preference is the
		// browser default; q-values beyond it are ignored.
		tag := strings.TrimSpace(strings.SplitN(part, ";", 2)[0])
		if tag != "" {
			return matchLang(tag)
		}
	}
	return defaultLang
}

// matchLang maps any BCP-47 tag onto a shipped language: an exact tag
// first, then its base language (de-CH → de), then the default.
func matchLang(tag string) string {
	parsed, err := language.Parse(tag)
	if err != nil {
		return language.English.String()
	}
	for _, t := range i18nLanguages {
		if t == parsed {
			return t.String()
		}
	}
	base, _ := parsed.Base()
	for _, t := range i18nLanguages {
		if bt, _ := t.Base(); bt == base {
			return t.String()
		}
	}
	return language.English.String()
}

// i18nDict is the wire shape of the /i18n responses and of window.I18N:
// the resolved language plus its complete message map (the English
// fallback already merged in, so the client never meets a missing key).
type i18nDict struct {
	Lang string            `json:"lang"`
	T    map[string]string `json:"t"`
}

// i18nCatalog is the YAML catalogs parsed into flat key→value maps plus
// the pre-built response bytes for the /i18n endpoints. Built once at
// process start, so the endpoints serve immutable bytes and a
// translation update is "edit the YAML, restart, done" — no generated
// files, nothing to drift.
type i18nCatalog struct {
	bytesByTag map[string][]byte
	keysByTag  map[string]map[string]string
}

var i18nData = buildI18nCatalog()

func buildI18nCatalog() *i18nCatalog {
	base := readCatalogFile(language.English.String())
	c := &i18nCatalog{
		bytesByTag: make(map[string][]byte, len(i18nLanguages)),
		keysByTag:  make(map[string]map[string]string, len(i18nLanguages)),
	}
	for _, t := range i18nLanguages {
		s := t.String()
		m := make(map[string]string, len(base))
		for k, v := range base {
			m[k] = v
		}
		if s != language.English.String() {
			for k, v := range readCatalogFile(s) {
				m[k] = v
			}
		}
		c.keysByTag[s] = m
		b, err := json.Marshal(i18nDict{Lang: s, T: m})
		if err != nil {
			log.Fatalf("i18n: marshaling %s dictionary: %v", s, err)
		}
		c.bytesByTag[s] = b
	}
	return c
}

// readCatalogFile parses one YAML catalog into a flat key→value map.
// Failing loudly here (process start) is deliberate: a broken catalog
// must never serve half-translated pages.
func readCatalogFile(tag string) map[string]string {
	raw, err := i18nFS.ReadFile("locales/" + tag + ".yaml")
	if err != nil {
		log.Fatalf("i18n: reading %s.yaml: %v", tag, err)
	}
	m := map[string]string{}
	if err := yaml.Unmarshal(raw, &m); err != nil {
		log.Fatalf("i18n: parsing %s.yaml: %v", tag, err)
	}
	return m
}

// i18nBootHandler serves the page's initial dictionary as a same-origin
// script (the site's CSP allows no inline scripts): the head loads it
// before the body, so the picker widgets already find their language on
// first render.
func i18nBootHandler(c fiber.Ctx) error {
	lang := langHandler(c, language.English.String())
	c.Set("Content-Type", "text/javascript; charset=utf-8")
	c.Set("Cache-Control", "no-cache")
	return c.Send(append([]byte("window.I18N = "), i18nData.bytesByTag[lang]...))
}

// i18nDictHandler serves the dictionary for one language — the client
// swapper fetches it on a language switch. The tag may be any BCP-47 tag
// the browser reports (de-DE, fr-FR, ...): it resolves onto a shipped
// language, so an unshipped language simply gets the English dictionary.
// A malformed tag 404s.
func i18nDictHandler(c fiber.Ctx) error {
	raw := c.Params("lang")
	if _, err := language.Parse(raw); err != nil {
		return c.SendStatus(404)
	}
	lang := matchLang(raw)
	c.Set("Content-Type", "application/json; charset=utf-8")
	c.Set("Cache-Control", "no-cache")
	return c.Send(i18nData.bytesByTag[lang])
}
