package main

import (
	_ "embed"
	"errors"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	// Bundle Go's zoneinfo so time.LoadLocation works on hosts without
	// system tzdata (Alpine, minimal containers).
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/gofiber/template/html/v3"
	"golang.org/x/text/language"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// db is the storage backend for the whole app. Handlers talk to storage
// only through it; tests swap it for a throwaway instance per run.
var db store.MeetingStore

// Runtime configuration, read once from the environment in loadConfig.
// devMode enables template reload + parse logging (off by default);
// baseURL is the public origin (KIUSAN_BASE_URL) for the copyable link
// boxes, empty falls back to the request's scheme+host; secureCookies sets
// the Secure cookie flag, derived from baseURL being https.
var (
	devMode       bool
	baseURL       string
	secureCookies bool
)

// Per-client rate limits on the unauthenticated write routes.
const (
	rateWindow         = time.Minute
	createMaxPerMinute = 20
	answerMaxPerMinute = 30
)

// loadConfig reads the runtime configuration from the environment.
func loadConfig() {
	baseURL = strings.TrimRight(os.Getenv("KIUSAN_BASE_URL"), "/")
	secureCookies = strings.HasPrefix(baseURL, "https://")
	switch strings.ToLower(os.Getenv("KIUSAN_DEV")) {
	case "1", "true", "yes", "on":
		devMode = true
	}
}

//go:embed data/timezones.txt
var timezoneNamesRaw string

// timezoneList is the IANA zones offered in the form's timezone select:
// every zone from the committed data/timezones.txt (curated list, grouped
// by UTC offset) that this build can actually load, so an offered zone can
// never fail to load in the meeting view. Sorted alphabetically for the
// dropdown, independent of the file's grouping. Embedded rather than read
// from disk so it works regardless of the working directory (tests chdir).
var timezoneList = func() []string {
	zones := loadableTimezones(strings.Split(timezoneNamesRaw, "\n"))
	sort.Strings(zones)
	return zones
}()

// loadableTimezones drops names the server cannot load; the list is
// validated once at process start, not per request. Blank lines and `#`
// group-comment lines in the data file are skipped.
func loadableTimezones(names []string) []string {
	zones := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" || strings.HasPrefix(n, "#") {
			continue
		}
		if _, err := time.LoadLocation(n); err != nil {
			log.Printf("warning: dropping unloadable timezone %q: %v", n, err)
			continue
		}
		zones = append(zones, n)
	}
	return zones
}

func initRoutes(app *fiber.App, staticDir string) {
	// Security headers on every response: refuse to render inside any frame
	// (clickjacking), a strict same-origin CSP (the site uses no inline
	// scripts or styles), no MIME sniffing, and a minimal referrer policy.
	app.Use(func(c fiber.Ctx) error {
		c.Set("X-Frame-Options", "DENY")
		c.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self'; "+
				"img-src 'self' data:; object-src 'none'; base-uri 'self'; "+
				"frame-ancestors 'none'")
		c.Set("X-Content-Type-Options", "nosniff")
		c.Set("Referrer-Policy", "same-origin")
		return c.Next()
	})

	// The i18n endpoints: the page's initial dictionary as a same-origin
	// script (the CSP allows no inline scripts) and the per-language
	// dictionaries the dropdown fetches on a switch.
	app.Get("/i18n/boot", i18nBootHandler)
	app.Get("/i18n/:lang", i18nDictHandler)

	// Make sure style files, fonts, and favicons are available
	// (served from the site root; a file miss resets the response and calls Next)
	app.Get("/*", static.New(filepath.Join(staticDir, "css")))
	app.Get("/*", static.New(filepath.Join(staticDir, "js")))
	app.Get("/*", static.New(filepath.Join(staticDir, "fonts")))
	app.Get("/*", static.New(filepath.Join(staticDir, "favicon")))

	// Rate limits on the unauthenticated write routes: poll creation and
	// answering are open, so bound the per-client request rate. Per-poll
	// cost is already capped (slot count, field lengths); this caps the rate.
	createLimit := limiter.New(limiter.Config{Max: createMaxPerMinute, Expiration: rateWindow})
	answerLimit := limiter.New(limiter.Config{Max: answerMaxPerMinute, Expiration: rateWindow})

	// Normal routes
	app.Get("/", homeHandler)
	app.Get("/1-1-form", formHandler)
	app.Post("/1-1-form", createLimit, formPostHandler)
	// The edit form is reachable by the admin link only.
	app.Get("/1-1-form/edit/+", formEditHandler)

	// Meeting page routes; the gobbler takes either link key of the poll.
	app.Get("/meet/+", meetingHandler)
	app.Post("/meet/+/answer", answerLimit, answerPostHandler)
	app.Post("/meet/+/answer/edit", answerLimit, answerEditPostHandler)
	app.Post("/meet/+/answer/delete", answerLimit, answerDeletePostHandler)

	// 404 Handler
	app.Use(func(c fiber.Ctx) error {
		return c.SendStatus(404) // => 404 "Not Found"
	})
}

// newApp wires up the template engine, middleware and routes. staticDir is
// the directory holding the templates and the css/favicon assets; callers may
// pass an absolute path to decouple the app from the working directory.
func newApp(staticDir string) *fiber.App {
	// We use html template engine
	engine := html.New(staticDir, ".html")

	// Dropdown labels show zone names with underscores as spaces
	// ("America/New York"); option values keep the real IANA name so
	// the form still submits a loadable zone.
	engine.AddFunc("zoneDisplay", func(z string) string {
		return strings.ReplaceAll(z, "_", " ")
	})

	// Template reload and parse logging are development conveniences, off by
	// default (enable with KIUSAN_DEV); reload re-reads the templates from
	// disk on every render.
	engine.Reload(devMode)
	engine.Debug(devMode)

	// Create app
	app := fiber.New(fiber.Config{
		Views:       engine,
		ReadTimeout: 10 * time.Second, // Don't keep cons alive for too long
		IdleTimeout: 60 * time.Second, // Drop idle keep-alive connections
	})

	// Logging middleware; the random link keys (capability URLs) are never
	// written to the log, so an exposed log can't be used to reach polls.
	app.Use(logger.New(logger.Config{
		Skip: func(c fiber.Ctx) bool {
			p := c.Path()
			return strings.HasPrefix(p, "/meet/") ||
				strings.HasPrefix(p, "/1-1-form/edit/")
		},
	}))

	// Set up all routes
	initRoutes(app, staticDir)

	return app
}

func main() {
	loadConfig()

	// Open the storage backend selected by KIUSAN_STORE / KIUSAN_DSN.
	var err error
	db, err = store.Open(os.Getenv("KIUSAN_STORE"), os.Getenv("KIUSAN_DSN"))
	if err != nil {
		log.Fatalf("Could not open store: %s", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Println("Store Shutdown:", err)
		}
	}()

	// Create app
	app := newApp("./static")

	// Shutting down section
	// Wait for interrupt signal to gracefully shutdown the server.
	// Must be registered before Listen, since Listen blocks.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	// Start the server and listen on http connections
	go func() {
		<-quit
		log.Println("Shutdown Server ...")
		// Shut down server
		if err := app.Shutdown(); err != nil {
			log.Println("Server stopped with error:", err)
		}
	}()

	if err := app.Listen(":8080"); err != nil {
		log.Println("Server stopped with error:", err)
	}

	log.Println("Server exiting")
}

func notFoundHandler(c fiber.Ctx) error {
	return c.SendStatus(404)
}

// sendErr writes a plain-text error body (never HTML-sniffable) with the
// given status. The message must be server-authored, never echoed input.
func sendErr(c fiber.Ctx, status int, msg string) error {
	c.Set("Content-Type", "text/plain; charset=utf-8")
	return c.Status(status).SendString(msg)
}

// sendInternal logs the real error and returns a generic 500, so driver
// and storage details never reach the client.
func sendInternal(c fiber.Ctx, err error) error {
	log.Printf("internal error: %v", err)
	c.Set("Content-Type", "text/plain; charset=utf-8")
	return c.Status(500).SendString("Internal server error")
}

// pageData is the binding every page renders through the home layout. A
// typed struct (not a fiber.Map) so the templates can call the T method
// with arguments — text/template only lets FuncMap functions and methods
// take arguments, not function values in a data map. c stays unexported:
// it feeds T, the templates never see it.
type pageData struct {
	c fiber.Ctx

	// i18n base, filled by renderPage.
	Lang          string
	TitleKey      string
	TitleSuffix   string
	PageTitle     string
	Metadesc      string
	LocaleChosen  string
	LocaleOptions []localeOption

	// 1-1 form (create + edit).
	Timezones    []string
	MeetingKey   string
	Title        string
	Email        string
	Description  string
	Timezone     string
	ExtraTz      string
	HideUsers    bool
	SingleSignup bool
	SlotsAttr    string
	FormTitleKey string
	Expiry       string
	DefaultExp   string
	MinExp       string
	CalPrev      string
	CalNext      string
	SubmitLabel  string

	// Meeting page.
	Tz                string
	TzName            string
	Ends              string
	EndsKey           string
	Ended             bool
	IsAdmin           bool
	UserLink          string
	AdminLink         string
	EditLink          string
	Single            bool
	Slots             []formSlot
	Header            []headerDate
	HeaderYears       []headerYear
	HasTime           bool
	Cols              []respCol
	SelfLink          string
	FormNew           bool
	FormID            string
	FormName          string
	FormAction        string
	FormRespondentID  string
	DeleteAction      string
	AnswerCSRF        bool
	DelCSRF           bool
	CSRFToken         string
	CopyTitle         string
	TableAria         string
	NameAria          string
	EditAnswer        string
	DeleteAnswer      string
	NamePh            string
	SubmitAnswerKey   string
	SubmitAnswerLabel string
}

// T translates one catalog key in the viewer's language for this
// request; the templates call it as {{.T "key"}}.
func (p *pageData) T(id string) string {
	return translator.MustLocalize(p.c, id)
}

// renderPage renders a page through the home layout, filling the i18n
// base every page needs: the viewer's language (html lang attribute),
// the <title> (catalog key + optional user suffix), the localized meta
// description, and the language dropdown state. titleSuffix is the
// user's poll title on the two dynamic titles (meet page, edit form)
// and "" elsewhere.
func renderPage(c fiber.Ctx, page, titleKey, titleSuffix string, d *pageData) error {
	d.c = c
	d.Lang = langHandler(c, "en")
	d.TitleKey = titleKey
	d.TitleSuffix = titleSuffix
	d.PageTitle = translator.MustLocalize(c, titleKey) + titleSuffix
	d.Metadesc = translator.MustLocalize(c, "meta.description")
	d.LocaleChosen = localeChosen(c)
	d.LocaleOptions = localeOptions
	return c.Render(page, d, "layouts/home")
}

// localeChosen is the language the dropdown marks selected: the resolved
// cookie choice, "" while the browser default is in effect (so the
// "Browser default" row stays marked).
func localeChosen(c fiber.Ctx) string {
	v := c.Cookies(localeCookieName)
	if v == "" {
		return ""
	}
	if _, err := language.Parse(v); err != nil {
		return ""
	}
	return matchLang(v)
}

func homeHandler(c fiber.Ctx) error {
	return renderPage(c, "index", "pagetitle.home", "", &pageData{})
}

// ---- 1-1 form: create & edit ----

func formHandler(c fiber.Ctx) error {
	now := time.Now()
	return renderPage(c, "1-1-form", "pagetitle.form", "", &pageData{
		Timezones:    timezoneList,
		FormTitleKey: "form.createTitle",
		DefaultExp:   now.UTC().AddDate(0, 3, 0).Format("2006-01-02"),
		MinExp:       now.UTC().Format("2006-01-02"),
		// Attribute-context strings, precomputed: a literal inside a
		// {{...}} action in an attribute value breaks html/template's
		// tokenization.
		CalPrev:     translator.MustLocalize(c, "form.calPrev"),
		CalNext:     translator.MustLocalize(c, "form.calNext"),
		SubmitLabel: translator.MustLocalize(c, "form.submit"),
	})
}

// formEditHandler renders the 1-1 form prefilled with the poll's stored
// entries (admin link only), so editing a poll keeps both of its links.
func formEditHandler(c fiber.Ctx) error {
	m, err := db.GetAdmin(c.Params("+"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notFoundHandler(c)
		}
		return sendInternal(c, err)
	}

	// The picker state the poll was created with: its slots in the
	// meeting's zone, one entry per slot — "YYYY-MM-DD" for midnight
	// slots, "YYYY-MM-DDTHH:MM" for timed ones. The picker groups them
	// by date and sorts.
	loc, lerr := time.LoadLocation(m.TimeZone)
	if lerr != nil {
		loc = time.UTC
	}
	slots := make([]string, 0, len(m.Dates))
	for _, d := range m.Dates {
		t := d.In(loc)
		if t.Hour() != 0 || t.Minute() != 0 {
			slots = append(slots, t.Format("2006-01-02T15:04"))
		} else {
			slots = append(slots, t.Format("2006-01-02"))
		}
	}

	expiry := m.Expires.In(loc).Format("2006-01-02")

	return renderPage(c, "1-1-form", "pagetitle.formEdit", m.Title, &pageData{
		Timezones:    timezoneList,
		MeetingKey:   m.AdminKey,
		Title:        m.Title,
		Email:        m.Email,
		Description:  m.Description,
		Timezone:     m.TimeZone,
		ExtraTz:      extraTimezone(m.TimeZone),
		HideUsers:    m.HideUsers,
		SingleSignup: m.SingleSignup,
		SlotsAttr:    strings.Join(slots, ","),
		FormTitleKey: "form.editTitle",
		Expiry:       expiry,
		MinExp:       time.Now().UTC().Format("2006-01-02"),
		// Attribute-context strings, precomputed (see formHandler).
		CalPrev:     translator.MustLocalize(c, "form.calPrev"),
		CalNext:     translator.MustLocalize(c, "form.calNext"),
		SubmitLabel: translator.MustLocalize(c, "form.saveChanges"),
	})
}

// extraTimezone returns the meeting's zone when it is not among the
// curated options, so the dropdown still carries a selected entry for it.
func extraTimezone(zone string) string {
	if zone == "" {
		return ""
	}
	for _, z := range timezoneList {
		if z == zone {
			return ""
		}
	}
	return zone
}

// formPostHandler creates a poll, or — when the form carries a meetingkey
// (an admin link key) — updates that poll in place, keeping both of its
// links.
func formPostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	form := new(database.Meeting)

	//// Get the form data
	// General fields
	if err := c.Bind().Body(form); err != nil {
		return sendErr(c, 400, "Invalid form data")
	}
	// Cap the free-text fields so one submission can't bloat storage or
	// the rendered page.
	if msg := validateFieldLengths(form); msg != "" {
		return sendErr(c, 400, msg)
	}

	// The two poll options; read explicitly so the checkbox values never
	// have to satisfy the struct binder.
	form.HideUsers = c.FormValue("hide-users") != ""
	form.SingleSignup = c.FormValue("single-signup") != ""

	// Timezone the meeting's dates/times are authored and displayed in;
	// optional, an empty value renders as UTC.
	form.TimeZone = strings.TrimSpace(c.FormValue("timezone"))
	loc, err := time.LoadLocation(form.TimeZone)
	if err != nil {
		return sendErr(c, 400, "Invalid timezone")
	}

	// Candidate slots, one per field: "YYYY-MM-DD" (a date-only slot,
	// midnight in the meeting zone) or "YYYY-MM-DDTHH:MM" (a timed slot).
	// Each date may carry any set of times; the picker sends them per
	// date.
	for i := 0; ; i++ {
		v := c.FormValue("slot" + strconv.Itoa(i))
		if v == "" {
			break
		}
		if i >= maxCandidateSlots {
			return sendErr(c, 400, "Too many candidate slots (max 100)")
		}
		datePart, timePart, _ := strings.Cut(v, "T")

		// HTML date inputs submit YYYY-MM-DD
		newDate, err := time.Parse("2006-01-02", datePart)
		if err != nil {
			return sendErr(c, 400, "Invalid date")
		}
		if timePart == "" {
			form.Dates = append(form.Dates,
				time.Date(newDate.Year(), newDate.Month(), newDate.Day(), 0, 0, 0, 0, loc))
			continue
		}
		// HTML time inputs submit HH:MM
		newTime, err := time.Parse("15:04", timePart)
		if err != nil {
			return sendErr(c, 400, "Invalid time")
		}
		form.Dates = append(form.Dates,
			time.Date(newDate.Year(), newDate.Month(), newDate.Day(),
				newTime.Hour(), newTime.Minute(), 0, 0, loc))
	}

	// End date (form field "enddate" — deliberately not named after
	// Meeting.Expires, whose binding the date-only value could not feed).
	// Empty means the 3-month default.
	endDate, msg := parseEndDate(c.FormValue("enddate"), time.Now())
	if msg != "" {
		return sendErr(c, 400, msg)
	}
	form.Expires = endOf(endDate, loc)

	//// update in place or create
	if editKey := strings.TrimSpace(c.FormValue("meetingkey")); editKey != "" {
		// Admin link only: the user link is public and must not unlock
		// poll editing.
		saved, err := saveMeeting(editKey, true, func(m *database.Meeting) {
			m.Title = form.Title
			m.Description = form.Description
			m.Email = form.Email
			m.HideUsers = form.HideUsers
			m.SingleSignup = form.SingleSignup
			m.TimeZone = form.TimeZone
			m.Dates = form.Dates
			m.Expires = form.Expires
			pruneAnswers(m)
		})
		if err != nil {
			return meetingSaveError(c, err)
		}
		// Both links survive the edit; back to the admin page.
		return c.Redirect().To("/meet/" + saved.AdminKey)
	}

	if _, err := db.Create(form); err != nil {
		return sendInternal(c, err)
	}
	// New poll: straight to its admin page, where both links live.
	return c.Redirect().To("/meet/" + form.AdminKey)
}

// parseEndDate parses the form's end date (YYYY-MM-DD, empty = the
// 3-month default) and rejects dates before today.
func parseEndDate(raw string, now time.Time) (time.Time, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return now.AddDate(0, 3, 0), ""
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		return time.Time{}, "Invalid end date"
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if d.Before(today) {
		return time.Time{}, "End date is in the past"
	}
	// Cap the horizon so a poll can't be set to expire in the far future.
	if d.After(today.AddDate(5, 0, 0)) {
		return time.Time{}, "End date is too far in the future"
	}
	return d, ""
}

// endOf turns a calendar end date into the instant the poll stops
// accepting answers: the end of that day in the meeting's zone.
func endOf(d time.Time, loc *time.Location) time.Time {
	return time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, loc)
}

// Poll size limits: they bound what one submission can store and render,
// so a single poll can't bloat the database or its page.
const (
	maxCandidateSlots = 100
	maxTitleLen       = 100
	maxEmailLen       = 320
	maxDescriptionLen = 5000
)

// validateFieldLengths caps the poll's free-text fields.
func validateFieldLengths(m *database.Meeting) string {
	switch {
	case utf8.RuneCountInString(m.Title) > maxTitleLen:
		return "Title is too long (max 100 characters)"
	case utf8.RuneCountInString(m.Email) > maxEmailLen:
		return "Email is too long (max 320 characters)"
	case utf8.RuneCountInString(m.Description) > maxDescriptionLen:
		return "Description is too long (max 5000 characters)"
	}
	return ""
}

// meetingSaveError maps the saveMeeting outcomes onto responses.
func meetingSaveError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errNotFoundPoll):
		return notFoundHandler(c)
	case errors.Is(err, errConflict):
		return sendErr(c, 500, "Too many concurrent edits, try again")
	}
	return sendInternal(c, err)
}

// ---- poll page ----

// The answers table's column slot: the slot's absolute instant as the
// form row's field name, plus the red button's pressed state (a slot the
// form target has not marked is red on submit).
type formSlot struct {
	Key    string
	PressR string
}

// The table header's cells: a year row (each year cell spanning that
// year's date columns), a date row (day/month/weekday in as few lines as
// the narrowest date column allows — consistently, on every date — each
// cell spanning that date's time columns), and a time sub-row when any
// slot has a time.
type timeCell struct {
	Slot string // "" for a midnight slot in a timed poll (renders the dash)
	Text string
}

type headerYear struct {
	year  int
	Label string
	Slot  string // first slot of the year (the client formats the label)
	Span  int
}

type headerDate struct {
	dayKey string
	day    time.Time
	Label  string // day/month/weekday, newline-separated per line
	Title  string // tooltip: "Mon 30 Nov 2026"
	Slot   string // first slot of the date (the client formats the label)
	Lines  int    // label line count 1|2|3 (data-lines)
	Span   int
	Times  []timeCell
}

// One visible respondent's row: their name, the per-slot marks (the form
// row's hidden-input values), the mark glyphs for a static row's cells,
// and the mark buttons' pressed states.
type respCol struct {
	ID, Name string
	IsOwn    bool
	Marks    []string // per slot: "g" | "y" | "" (the form row's hidden-input values)
	Syms     []string // per slot: the mark glyph in a static row's cell
	Csls     []string // per slot: st-g | st-y | st-r
	PressG   []string
	PressY   []string
	PressR   []string
}

// meetingHandler renders the poll page for a user or admin link: the same
// page, with the admin link adding the copyable links, the edit-poll
// button and the per-row edit/delete icons on every answer.
func meetingHandler(c fiber.Ctx) error {
	m, isAdmin, err := resolveMeeting(c.Params("+"))
	if err != nil {
		return sendInternal(c, err)
	}
	if m == nil {
		return notFoundHandler(c)
	}

	now := time.Now()
	ended := pollEnded(m, now)
	identity := ensureIdentityCookie(c, m, now)
	own := findAnswer(m.Answers, identity)

	// The one form row in the answers table: the empty last row while the
	// viewer has no answer yet, or — opened with ?edit=<id> — that
	// respondent's row turned into the form in place. A user link may only
	// open its own row; an admin link any row, with someone else's row
	// posting through the capability admin edit endpoint (the own row
	// posts through the cookie identity like a respondent's).
	viewKey := m.Key
	if isAdmin {
		viewKey = m.AdminKey
	}
	editParam := strings.TrimSpace(c.Query("edit"))
	var other *database.Answer
	if editParam != "" {
		other = findAnswer(m.Answers, editParam)
	}
	var formTarget *database.Answer
	formID := ""
	formNew := false
	formAction := "/meet/" + viewKey + "/answer"
	formRespondentID := ""
	if !ended {
		switch {
		case own != nil && editParam == identity:
			formTarget = own
			formID = own.ID
		case isAdmin && other != nil && other.ID != identity:
			formTarget = other
			formID = other.ID
			formAction = "/meet/" + m.AdminKey + "/answer/edit"
			formRespondentID = other.ID
		case own == nil:
			formNew = true
		}
	}
	formName := ""
	if formTarget != nil {
		formName = formTarget.Name
	}

	// All slots are shown in the meeting's timezone (UTC when none was
	// set) so the display never depends on the server's timezone.
	loc, lerr := time.LoadLocation(m.TimeZone)
	if lerr != nil {
		loc = time.UTC
	}
	// tz is the raw IANA name for the client's Intl formatting (data-tz);
	// tzName is the same name with underscores as spaces for display.
	tz := m.TimeZone
	if tz == "" {
		tz = "UTC"
	}
	tzName := strings.ReplaceAll(tz, "_", " ")
	ends := m.Expires.In(loc).Format("2006-01-02")
	endsKey := slotKey(m.Expires)

	// Display order: chronological. Storage order follows the form's chip
	// order, which can be out of order, and marks are keyed by the slot's
	// absolute instant, so sorting a display copy is safe.
	dates := make([]time.Time, len(m.Dates))
	copy(dates, m.Dates)
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	// Table column slots, prefilled with the form target's marks.
	press := func(on bool) string {
		if on {
			return "true"
		}
		return "false"
	}
	markSym := func(v string) (sym, cls string) {
		switch v {
		case "g":
			return "\u2713", "g"
		case "y":
			return "(\u2713)", "y"
		}
		return "\u2717", "r"
	}
	slots := make([]formSlot, 0, len(dates))
	for _, d := range dates {
		status := ""
		if formTarget != nil {
			status = formTarget.Marks[slotKey(d)]
		}
		slots = append(slots, formSlot{Key: slotKey(d), PressR: press(status == "")})
	}

	// Header rows: a year row (each year cell spanning that year's date
	// columns), a date row (day/month/weekday in as few lines as the
	// narrowest date column allows — consistently, on every date — each
	// cell spanning that date's time columns), and a time sub-row when any
	// slot has a time.
	headerYears := make([]headerYear, 0)
	header := make([]headerDate, 0)
	hasTime := false
	for _, d := range dates {
		t := d.In(loc)
		y := t.Year()
		if n := len(headerYears); n == 0 || headerYears[n-1].year != y {
			headerYears = append(headerYears, headerYear{year: y,
				Label: strconv.Itoa(int(y)), Slot: slotKey(d)})
		}
		headerYears[len(headerYears)-1].Span++
		dayKey := t.Format("2006-01-02")
		if n := len(header); n == 0 || header[n-1].dayKey != dayKey {
			header = append(header, headerDate{dayKey: dayKey, day: t,
				Title: t.Format("Mon 2 Jan 2006"), Slot: slotKey(d)})
		}
		g := &header[len(header)-1]
		g.Span++
		cell := timeCell{Text: "\u2014"} // —: a date-only slot in a timed poll
		if t.Hour() != 0 || t.Minute() != 0 {
			cell = timeCell{Slot: slotKey(d), Text: t.Format("15:04")}
			hasTime = true
		}
		g.Times = append(g.Times, cell)
	}
	// The date label uses as few lines as fit the narrowest date column
	// (without stretching the 44px slot columns), applied to every date
	// so the header reads consistently: three lines when any date has one
	// slot column, two when the narrowest has two, one when every date
	// has three or more.
	minSpan := 0
	for i := range header {
		if minSpan == 0 || header[i].Span < minSpan {
			minSpan = header[i].Span
		}
	}
	for i := range header {
		t := header[i].day
		switch {
		case minSpan <= 1:
			header[i].Lines = 3
			header[i].Label = t.Format("2") + "\n" +
				t.Format("Jan") + "\n" + t.Format("Mon")
		case minSpan == 2:
			header[i].Lines = 2
			header[i].Label = t.Format("2 Jan") + "\n" + t.Format("Mon")
		default:
			header[i].Lines = 1
			header[i].Label = t.Format("2 Jan Mon")
		}
	}

	// The respondents visible on this page.
	var shown database.AnswerList
	switch {
	case isAdmin || !m.HideUsers:
		shown = m.Answers
	case own != nil:
		shown = database.AnswerList{*own}
	}
	cols := make([]respCol, 0, len(shown))
	for i := range shown {
		marks := make([]string, len(dates))
		syms := make([]string, len(dates))
		csls := make([]string, len(dates))
		pg := make([]string, len(dates))
		py := make([]string, len(dates))
		pr := make([]string, len(dates))
		for j, d := range dates {
			v := shown[i].Marks[slotKey(d)]
			marks[j] = v
			s, cl := markSym(v)
			syms[j] = s
			csls[j] = cl
			pg[j] = press(v == "g")
			py[j] = press(v == "y")
			pr[j] = press(v == "")
		}
		cols = append(cols, respCol{ID: shown[i].ID, Name: shown[i].Name, IsOwn: shown[i].ID == identity,
			Marks: marks, Syms: syms, Csls: csls, PressG: pg, PressY: py, PressR: pr})
	}

	// The page's forms post through the same link the page is viewed on, so
	// the handlers' back-redirect lands on this page (the admin stays on the
	// admin page after answering, editing, or deleting an answer there).
	csrf := ""
	if identity != "" && !ended {
		csrf = csrfToken(identity)
	}
	submitKey := "meet.submitAnswer"
	if formID != "" {
		submitKey = "meet.saveChanges"
	}
	return renderPage(c, "meet", "pagetitle.meet", m.Title, &pageData{
		Title:            m.Title,
		Description:      m.Description,
		Email:            m.Email,
		Tz:               tz,
		TzName:           tzName,
		Ends:             ends,
		EndsKey:          endsKey,
		Ended:            ended,
		IsAdmin:          isAdmin,
		UserLink:         absURL(c, "/meet/"+m.Key),
		AdminLink:        absURL(c, "/meet/"+m.AdminKey),
		EditLink:         "/1-1-form/edit/" + m.AdminKey,
		Single:           m.SingleSignup,
		Slots:            slots,
		Header:           header,
		HeaderYears:      headerYears,
		HasTime:          hasTime,
		Cols:             cols,
		SelfLink:         "/meet/" + viewKey,
		FormNew:          formNew,
		FormID:           formID,
		FormName:         formName,
		FormAction:       formAction,
		FormRespondentID: formRespondentID,
		DeleteAction:     "/meet/" + viewKey + "/answer/delete",
		// CSRF travels with the forms that post through the cookie
		// identity: the form row (unless it posts through the capability
		// admin edit endpoint) and, in the user view, the delete form.
		AnswerCSRF: csrf != "" && formRespondentID == "" && (formNew || formID != ""),
		DelCSRF:    csrf != "" && !isAdmin,
		CSRFToken:  csrf,
		// Attribute-context strings, precomputed (see formHandler).
		CopyTitle:         translator.MustLocalize(c, "meet.copyTitle"),
		TableAria:         translator.MustLocalize(c, "meet.tableAria"),
		NameAria:          translator.MustLocalize(c, "meet.nameAria"),
		EditAnswer:        translator.MustLocalize(c, "meet.editAnswer"),
		DeleteAnswer:      translator.MustLocalize(c, "meet.deleteAnswer"),
		NamePh:            translator.MustLocalize(c, "meet.namePh"),
		SubmitAnswerKey:   submitKey,
		SubmitAnswerLabel: translator.MustLocalize(c, submitKey),
	})
}

// absURL renders a site-absolute URL for the copyable link boxes on the
// admin page. It prefers the configured public origin (KIUSAN_BASE_URL)
// so the links stay correct behind a proxy; without it, it falls back to
// the request's scheme+host.
func absURL(c fiber.Ctx, path string) string {
	if baseURL != "" {
		return baseURL + path
	}
	return c.Scheme() + "://" + c.Host() + path
}

// answerPostHandler stores or updates the answer of the respondent behind
// the request — the identity cookie; either link may submit it.
func answerPostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	m, _, err := resolveMeeting(c.Params("+"))
	if err != nil {
		return sendInternal(c, err)
	}
	if m == nil {
		return notFoundHandler(c)
	}
	if pollEnded(m, time.Now()) {
		return c.Status(410).SendString("This poll has ended")
	}
	identity := c.Cookies(cookieName(m))
	if identity == "" || !verifyCSRF(c, identity) {
		return c.Status(401).SendString("Unknown or stale identity — reload the page and try again")
	}
	name, msg := validName(c.FormValue("name"))
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	marks, msg := collectMarks(c, m)
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	if _, err := saveMeeting(m.Key, false, func(mm *database.Meeting) {
		mm.Answers = upsertAnswer(mm.Answers, identity, name, marks, time.Now())
	}); err != nil {
		return meetingSaveError(c, err)
	}
	return c.Redirect().To("/meet/" + c.Params("+"))
}

// answerEditPostHandler lets the admin (admin link only) edit any
// respondent's answer.
func answerEditPostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	m, isAdmin, err := resolveMeeting(key)
	if err != nil {
		return sendInternal(c, err)
	}
	// The route exists for both link shapes; only the admin link may use
	// it, so a user key looks up like a missing link.
	if m == nil || !isAdmin {
		return notFoundHandler(c)
	}
	if pollEnded(m, time.Now()) {
		return c.Status(410).SendString("This poll has ended")
	}
	id := strings.TrimSpace(c.FormValue("respondentid"))
	if findAnswer(m.Answers, id) == nil {
		return notFoundHandler(c)
	}
	name, msg := validName(c.FormValue("name"))
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	marks, msg := collectMarks(c, m)
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	if _, err := saveMeeting(key, true, func(mm *database.Meeting) {
		mm.Answers = upsertAnswer(mm.Answers, id, name, marks, time.Now())
	}); err != nil {
		return meetingSaveError(c, err)
	}
	return c.Redirect().To("/meet/" + key)
}

// answerDeletePostHandler deletes an answer: via the admin link any
// respondent's; via the user link only the own (identity cookie) answer,
// which also expires the identity cookie.
func answerDeletePostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	m, isAdmin, err := resolveMeeting(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if m == nil {
		return notFoundHandler(c)
	}
	if pollEnded(m, time.Now()) {
		return c.Status(410).SendString("This poll has ended")
	}
	id := strings.TrimSpace(c.FormValue("respondentid"))
	if id == "" {
		return c.Status(400).SendString("Missing respondent")
	}
	identity := c.Cookies(cookieName(m))
	if !isAdmin && id != identity {
		// A user link may only ever act on the own identity.
		return notFoundHandler(c)
	}
	if findAnswer(m.Answers, id) == nil {
		return notFoundHandler(c)
	}
	if !isAdmin && !verifyCSRF(c, identity) {
		return c.Status(401).SendString("Unknown or stale identity — reload the page and try again")
	}
	if _, err := saveMeeting(key, isAdmin, func(mm *database.Meeting) {
		mm.Answers, _ = removeAnswer(mm.Answers, id)
	}); err != nil {
		return meetingSaveError(c, err)
	}
	if !isAdmin {
		dropIdentityCookie(c, m)
	}
	return c.Redirect().To("/meet/" + key)
}
