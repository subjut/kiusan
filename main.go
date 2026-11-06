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
var db store.Store

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

	// Vote form routes.
	app.Get("/choice-form", voteFormHandler)
	app.Post("/choice-form", createLimit, voteFormPostHandler)
	app.Get("/choice-form/edit/+", voteFormEditHandler)

	// Vote page routes; the gobbler takes either link key of the vote.
	app.Get("/vote/+", voteHandler)
	app.Post("/vote/+/answer", answerLimit, voteAnswerPostHandler)
	app.Post("/vote/+/answer/edit", answerLimit, voteAnswerEditPostHandler)
	app.Post("/vote/+/answer/delete", answerLimit, voteAnswerDeletePostHandler)

	// Vote lifecycle, admin link only.
	app.Post("/vote/+/close", voteClosePollHandler)
	app.Post("/vote/+/reopen", voteReopenPollHandler)
	app.Post("/vote/+/delete", voteDeletePollHandler)

	// Meeting page routes; the gobbler takes either link key of the poll.
	app.Get("/meet/+", meetingHandler)
	app.Post("/meet/+/answer", answerLimit, answerPostHandler)
	app.Post("/meet/+/answer/edit", answerLimit, answerEditPostHandler)
	app.Post("/meet/+/answer/delete", answerLimit, answerDeletePostHandler)

	// Poll lifecycle, admin link only: close and re-open stop and restore
	// answering, delete purges the poll and both of its links.
	app.Post("/meet/+/close", closePollHandler)
	app.Post("/meet/+/reopen", reopenPollHandler)
	app.Post("/meet/+/delete", deletePollHandler)

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

// meetingGone renders the 404 for a link that used to be a poll (or never
// was): a full page with its own message, distinct from the plain-text
// 404 of an unknown path. c.Render only sets the body and content type,
// so the status set here is the one sent.
func meetingGone(c fiber.Ctx) error {
	if err := renderPage(c, "meet-gone", "pagetitle.meetGone", "", &pageData{}); err != nil {
		return err
	}
	c.Status(404)
	return nil
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

	// themeClass: the viewer's explicit theme choice ("" / theme-dark /
	// theme-light on <html>), filled by renderPage.
	ThemeClass string

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

	// Vote form (create + edit). On edit the stored choices carry their
	// stable IDs, which the form renders as hidden fields so unchanged
	// options keep their ID (and their answers).
	VoteKey      string
	Choices      database.ChoiceList
	SingleChoice bool
	FreeText     bool

	// Vote page.
	ModeKey      string
	VoteFreeText bool
	VoteCols     []voteCol
	VoteFormCol  voteCol

	// Meeting page.
	Tz                string
	TzName            string
	Ends              string
	EndsKey           string
	Ended             bool
	Closed            bool
	IsAdmin           bool
	CloseAction       string
	ReopenAction      string
	DeletePollAction  string
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
	FormCol           respCol
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
// description, the language dropdown state, and the theme class.
// titleSuffix is the user's poll title on the two dynamic titles (meet
// page, edit form) and "" elsewhere.
func renderPage(c fiber.Ctx, page, titleKey, titleSuffix string, d *pageData) error {
	d.c = c
	d.Lang = langHandler(c, "en")
	d.TitleKey = titleKey
	d.TitleSuffix = titleSuffix
	d.PageTitle = translator.MustLocalize(c, titleKey) + titleSuffix
	d.Metadesc = translator.MustLocalize(c, "meta.description")
	d.LocaleChosen = localeChosen(c)
	d.LocaleOptions = localeOptions
	d.ThemeClass = themeClass(c)
	return c.Render(page, d, "layouts/home")
}

// themeClass is the <html> class carrying the viewer's explicit theme
// choice: the kiusan-theme cookie ("dark" / "light"). While no cookie is
// set the class stays absent and the CSS media query follows the system
// setting (static/js/theme.js mirrors a live choice onto the same class
// and persists it to the cookie on the toggle click).
func themeClass(c fiber.Ctx) string {
	switch c.Cookies("kiusan-theme") {
	case "dark":
		return "theme-dark"
	case "light":
		return "theme-light"
	default:
		return ""
	}
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
			// The admin link of a deleted poll renders the gone page.
			return meetingGone(c)
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

// ---- vote form: create & edit ----

func voteFormHandler(c fiber.Ctx) error {
	now := time.Now()
	return renderPage(c, "choice-form", "pagetitle.voteForm", "", &pageData{
		FormTitleKey: "form.voteCreateTitle",
		DefaultExp:   now.UTC().AddDate(0, 3, 0).Format("2006-01-02"),
		MinExp:       now.UTC().Format("2006-01-02"),
		SubmitLabel:  translator.MustLocalize(c, "form.submit"),
	})
}

func voteFormEditHandler(c fiber.Ctx) error {
	v, err := db.GetVoteAdmin(c.Params("+"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return meetingGone(c)
		}
		return sendInternal(c, err)
	}

	return renderPage(c, "choice-form", "pagetitle.voteFormEdit", v.Title, &pageData{
		VoteKey:      v.AdminKey,
		Title:        v.Title,
		Email:        v.Email,
		Description:  v.Description,
		HideUsers:    v.HideUsers,
		SingleChoice: v.SingleChoice,
		FreeText:     v.FreeText,
		Choices:      v.Choices,
		FormTitleKey: "form.voteEditTitle",
		Expiry:       v.Expires.UTC().Format("2006-01-02"),
		MinExp:       time.Now().UTC().Format("2006-01-02"),
		SubmitLabel:  translator.MustLocalize(c, "form.saveChanges"),
	})
}

func voteFormPostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	var v database.Vote

	if err := c.Bind().Body(&v); err != nil {
		return sendErr(c, 400, "Invalid form data")
	}
	if msg := validateVoteFieldLengths(&v); msg != "" {
		return sendErr(c, 400, msg)
	}

	v.HideUsers = c.FormValue("hide-users") != ""
	v.SingleChoice = c.FormValue("single-choice") != ""
	v.FreeText = c.FormValue("free-text") != ""

	// The option rows arrive as (choice0, choiceID0), (choice1,
	// choiceID1), ... — on edit the ID is the one the form rendered, so a
	// row whose text is unchanged keeps its ID (and its answers).
	rows := make([]choiceRow, 0, 4)
	for i := 0; ; i++ {
		text := strings.TrimSpace(c.FormValue("choice" + strconv.Itoa(i)))
		if text == "" {
			break
		}
		if len(rows) >= maxChoices {
			return sendErr(c, 400, "Too many choices (max 20)")
		}
		if utf8.RuneCountInString(text) > maxChoiceLen {
			return sendErr(c, 400, "Choice too long (max 100 characters)")
		}
		rows = append(rows, choiceRow{
			text:  text,
			oldID: strings.TrimSpace(c.FormValue("choiceID" + strconv.Itoa(i))),
		})
	}
	if len(rows) == 0 {
		return sendErr(c, 400, "At least one choice is required")
	}

	endDate, msg := parseEndDate(c.FormValue("enddate"), time.Now())
	if msg != "" {
		return sendErr(c, 400, msg)
	}
	v.Expires = endOf(endDate, time.UTC)

	if editKey := strings.TrimSpace(c.FormValue("votekey")); editKey != "" {
		old, err := db.GetVoteAdmin(editKey)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return notFoundHandler(c)
			}
			return sendInternal(c, err)
		}
		choices, err := assignChoiceIDs(old.Choices, rows)
		if err != nil {
			return sendInternal(c, err)
		}
		v.Choices = choices
		saved, err := saveVote(editKey, true, func(vote *database.Vote) {
			vote.Title = v.Title
			vote.Description = v.Description
			vote.Email = v.Email
			vote.HideUsers = v.HideUsers
			vote.SingleChoice = v.SingleChoice
			vote.FreeText = v.FreeText
			vote.Choices = v.Choices
			vote.Expires = v.Expires
			pruneVoteAnswers(vote)
		})
		if err != nil {
			return voteSaveError(c, err)
		}
		return c.Redirect().To("/vote/" + saved.AdminKey)
	}

	choices, err := assignChoiceIDs(nil, rows)
	if err != nil {
		return sendInternal(c, err)
	}
	v.Choices = choices
	if _, err := db.CreateVote(&v); err != nil {
		return sendInternal(c, err)
	}
	return c.Redirect().To("/vote/" + v.AdminKey)
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
	maxChoices        = 20
	maxChoiceLen      = 100
	maxVoteNoteLen    = 150
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

// validateVoteFieldLengths caps the vote's free-text fields.
func validateVoteFieldLengths(v *database.Vote) string {
	switch {
	case utf8.RuneCountInString(v.Title) > maxTitleLen:
		return "Title is too long (max 100 characters)"
	case utf8.RuneCountInString(v.Email) > maxEmailLen:
		return "Email is too long (max 320 characters)"
	case utf8.RuneCountInString(v.Description) > maxDescriptionLen:
		return "Description is too long (max 5000 characters)"
	}
	return ""
}

// ---- poll page ----

// The answers table's column slot: the slot's absolute instant as the
// form row's field name, plus the red button's pressed state (a slot the
// form target has not marked is red on submit).
type formSlot struct {
	Key string
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
	Csls     []string // per slot: g | y | r (static row cells: r renders red + ✗)
	Fcls     []string // per slot: st-g | st-y | "" (form cells: unmarked stays neutral)
	PressG   []string
	PressY   []string
}

// One visible voter's row on a vote: the per-choice marks (mark mode) or
// notes (free-text mode), the per-choice display glyph, and the mark
// buttons' pressed states. Marks are normalized for single-choice votes,
// where an unvoted option counts as no.
type voteCol struct {
	ID, Name string
	IsOwn    bool
	Marks    []string // per choice: "y" | "m" | "n" | "" (form row hidden-input values)
	Notes    []string // per choice: the free-text value
	Syms     []string // per choice: the display glyph (or note text)
	Csls     []string // per choice: g | y | r | ""
	Fcls     []string // per choice: st-g | st-y | st-r | ""
	PressY   []string
	PressM   []string
	PressN   []string
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
		// A link that resolves to no poll — deleted, or never created —
		// gets the gone page, a 404 with its own message.
		return meetingGone(c)
	}

	now := time.Now()
	ended := pollEnded(m, now)
	closed := m.Closed
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
	if !ended && !closed {
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
		slots = append(slots, formSlot{Key: slotKey(d)})
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
		fcls := make([]string, len(dates))
		pg := make([]string, len(dates))
		py := make([]string, len(dates))
		for j, d := range dates {
			v := shown[i].Marks[slotKey(d)]
			marks[j] = v
			s, cl := markSym(v)
			syms[j] = s
			csls[j] = cl
			if cl != "r" {
				fcls[j] = "st-" + cl // form cells: unmarked stays neutral, no red
			}
			pg[j] = press(v == "g")
			py[j] = press(v == "y")
		}
		cols = append(cols, respCol{ID: shown[i].ID, Name: shown[i].Name, IsOwn: shown[i].ID == identity,
			Marks: marks, Syms: syms, Csls: csls, Fcls: fcls, PressG: pg, PressY: py})
	}

	// The form row renders as the table's last row (next to the save/
	// cancel bar below the answers): the edited respondent's row is
	// skipped in the loop above and carried here, prefilled. The row is
	// found by id because cols' order follows the visible respondents.
	var formCol respCol
	if formID != "" {
		for _, col := range cols {
			if col.ID == formID {
				formCol = col
				break
			}
		}
	}

	// The page's forms post through the same link the page is viewed on, so
	// the handlers' back-redirect lands on this page (the admin stays on the
	// admin page after answering, editing, or deleting an answer there).
	csrf := ""
	if identity != "" && !ended && !closed {
		csrf = csrfToken(identity)
	}
	submitKey := "meet.submitAnswer"
	if formID != "" {
		submitKey = "meet.saveChanges"
	}
	return renderPage(c, "meet", "pagetitle.meet", m.Title, &pageData{
		Title:       m.Title,
		Description: m.Description,
		Email:       m.Email,
		Tz:          tz,
		TzName:      tzName,
		Ends:        ends,
		EndsKey:     endsKey,
		Ended:       ended,
		Closed:      closed,
		IsAdmin:     isAdmin,
		UserLink:    absURL(c, "/meet/"+m.Key),
		AdminLink:   absURL(c, "/meet/"+m.AdminKey),
		EditLink:    "/1-1-form/edit/" + m.AdminKey,
		// The lifecycle forms post through the admin link (a
		// capability URL) — they only render in the admin view.
		CloseAction:      "/meet/" + m.AdminKey + "/close",
		ReopenAction:     "/meet/" + m.AdminKey + "/reopen",
		DeletePollAction: "/meet/" + m.AdminKey + "/delete",
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
		FormCol:          formCol,
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
	if m.Closed || pollEnded(m, time.Now()) {
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
	if m.Closed || pollEnded(m, time.Now()) {
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
	if m.Closed || pollEnded(m, time.Now()) {
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

// ---- poll lifecycle (admin link only) ----

// closePollHandler closes a running poll: it stops accepting answers until
// the admin re-opens it, while both links keep rendering the answers table.
func closePollHandler(c fiber.Ctx) error {
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
	if _, err := saveMeeting(key, true, func(mm *database.Meeting) {
		mm.Closed = true
	}); err != nil {
		return meetingSaveError(c, err)
	}
	return c.Redirect().To("/meet/" + key)
}

// reopenPollHandler re-opens a closed poll: new answers can be submitted
// and existing ones edited or deleted again.
func reopenPollHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	m, isAdmin, err := resolveMeeting(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if m == nil || !isAdmin {
		return notFoundHandler(c)
	}
	if _, err := saveMeeting(key, true, func(mm *database.Meeting) {
		mm.Closed = false
	}); err != nil {
		return meetingSaveError(c, err)
	}
	return c.Redirect().To("/meet/" + key)
}

// ---- vote page ----

// voteHandler renders the vote page for a user or admin link: the same
// page, with the admin link adding the copyable links, the edit-vote
// button and the per-row edit/delete icons on every answer.
func voteHandler(c fiber.Ctx) error {
	v, isAdmin, err := resolveVote(c.Params("+"))
	if err != nil {
		return sendInternal(c, err)
	}
	if v == nil {
		return meetingGone(c)
	}

	now := time.Now()
	ended := voteEnded(v, now)
	closed := v.Closed
	identity := ensureVoteIdentityCookie(c, v, now)
	own := findVoteAnswer(v.Answers, identity)

	// The one form row in the answers table: the empty last row while the
	// viewer has no answer yet, or — opened with ?edit=<id> — that voter's
	// row turned into the form in place. A user link may only open its own
	// row; an admin link any row.
	viewKey := v.Key
	if isAdmin {
		viewKey = v.AdminKey
	}
	editParam := strings.TrimSpace(c.Query("edit"))
	var other *database.VoteAnswer
	if editParam != "" {
		other = findVoteAnswer(v.Answers, editParam)
	}
	var formTarget *database.VoteAnswer
	formID := ""
	formNew := false
	formAction := "/vote/" + viewKey + "/answer"
	formRespondentID := ""
	if !ended && !closed {
		switch {
		case own != nil && editParam == identity:
			formTarget = own
			formID = own.ID
		case isAdmin && other != nil && other.ID != identity:
			formTarget = other
			formID = other.ID
			formAction = "/vote/" + v.AdminKey + "/answer/edit"
			formRespondentID = other.ID
		case own == nil:
			formNew = true
		}
	}
	formName := ""
	if formTarget != nil {
		formName = formTarget.Name
	}

	n := len(v.Choices)
	press := func(on bool) string {
		if on {
			return "true"
		}
		return "false"
	}
	markGlyph := func(mk string) string {
		switch mk {
		case "y":
			return "\u2713"
		case "m":
			return "(\u2713)"
		case "n":
			return "\u2717"
		}
		return ""
	}
	markClass := func(mk string) string {
		switch mk {
		case "y":
			return "g"
		case "m":
			return "y"
		case "n":
			return "r"
		}
		return ""
	}
	// displayState is one answer's per-choice display state: the note text
	// in free-text mode (an unvoted option stays empty — there is no
	// stored "no" to show), else the mark's glyph; single-choice mark
	// votes normalize an unvoted option to no.
	displayState := func(a database.VoteAnswer, j int) (string, string) {
		k := v.Choices[j].ID
		if v.FreeText {
			return a.Notes[k], ""
		}
		mk := a.Marks[k]
		if v.SingleChoice && mk == "" {
			mk = "n"
		}
		return markGlyph(mk), markClass(mk)
	}

	// The voters visible on this page.
	var shown database.VoteAnswerList
	switch {
	case isAdmin || !v.HideUsers:
		shown = v.Answers
	case own != nil:
		shown = database.VoteAnswerList{*own}
	}
	cols := make([]voteCol, 0, len(shown))
	for i := range shown {
		marks := make([]string, n)
		notes := make([]string, n)
		syms := make([]string, n)
		csls := make([]string, n)
		fcls := make([]string, n)
		py := make([]string, n)
		pm := make([]string, n)
		pn := make([]string, n)
		for j := 0; j < n; j++ {
			k := v.Choices[j].ID
			if v.FreeText {
				notes[j] = shown[i].Notes[k]
			} else {
				mk := shown[i].Marks[k]
				if v.SingleChoice && mk == "" {
					mk = "n"
				}
				marks[j] = mk
				if mk != "" {
					fcls[j] = "st-" + markClass(mk)
				}
			}
			s, cl := displayState(shown[i], j)
			syms[j] = s
			csls[j] = cl
			py[j] = press(marks[j] == "y")
			pm[j] = press(marks[j] == "m")
			pn[j] = press(marks[j] == "n")
		}
		cols = append(cols, voteCol{ID: shown[i].ID, Name: shown[i].Name, IsOwn: shown[i].ID == identity,
			Marks: marks, Notes: notes, Syms: syms, Csls: csls, Fcls: fcls,
			PressY: py, PressM: pm, PressN: pn})
	}

	// The form row renders as the table's last row (next to the save/
	// cancel bar below the answers): the edited voter's row is skipped in
	// the loop above and carried here, prefilled.
	var formCol voteCol
	if formID != "" {
		for _, col := range cols {
			if col.ID == formID {
				formCol = col
				break
			}
		}
	}

	csrf := ""
	if identity != "" && !ended && !closed {
		csrf = csrfToken(identity)
	}
	submitKey := "meet.submitAnswer"
	if formID != "" {
		submitKey = "meet.saveChanges"
	}
	modeKey := "vote.modeMarks"
	if v.FreeText {
		modeKey = "vote.modeFreeText"
	}
	ends := v.Expires.UTC().Format("2006-01-02")
	endsKey := slotKey(v.Expires)

	return renderPage(c, "vote", "pagetitle.meet", v.Title, &pageData{
		Title:       v.Title,
		Description: v.Description,
		Email:       v.Email,
		Ends:        ends,
		EndsKey:     endsKey,
		Ended:       ended,
		Closed:      closed,
		IsAdmin:     isAdmin,
		UserLink:    absURL(c, "/vote/"+v.Key),
		AdminLink:   absURL(c, "/vote/"+v.AdminKey),
		EditLink:    "/choice-form/edit/" + v.AdminKey,
		// The lifecycle forms post through the admin link (a
		// capability URL) — they only render in the admin view.
		CloseAction:      "/vote/" + v.AdminKey + "/close",
		ReopenAction:     "/vote/" + v.AdminKey + "/reopen",
		DeletePollAction: "/vote/" + v.AdminKey + "/delete",
		Single:           v.SingleChoice,
		Choices:          v.Choices,
		ModeKey:          modeKey,
		VoteFreeText:     v.FreeText,
		VoteCols:         cols,
		SelfLink:         "/vote/" + viewKey,
		FormNew:          formNew,
		FormID:           formID,
		FormName:         formName,
		FormAction:       formAction,
		FormRespondentID: formRespondentID,
		VoteFormCol:      formCol,
		DeleteAction:     "/vote/" + viewKey + "/answer/delete",
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

// voteAnswerPostHandler stores or updates the answer of the voter behind
// the request — the identity cookie; either link may submit it.
func voteAnswerPostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	v, _, err := resolveVote(c.Params("+"))
	if err != nil {
		return sendInternal(c, err)
	}
	if v == nil {
		return notFoundHandler(c)
	}
	if v.Closed || voteEnded(v, time.Now()) {
		return c.Status(410).SendString("This vote has ended")
	}
	identity := c.Cookies(voteCookieName(v))
	if identity == "" || !verifyCSRF(c, identity) {
		return c.Status(401).SendString("Unknown or stale identity — reload the page and try again")
	}
	name, msg := validName(c.FormValue("name"))
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	marks, notes, msg := collectVoteAnswers(c, v)
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	if _, err := saveVote(v.Key, false, func(vt *database.Vote) {
		vt.Answers = upsertVoteAnswer(vt.Answers, identity, name, marks, notes, time.Now())
	}); err != nil {
		return voteSaveError(c, err)
	}
	return c.Redirect().To("/vote/" + c.Params("+"))
}

// voteAnswerEditPostHandler lets the admin (admin link only) edit any
// voter's answer.
func voteAnswerEditPostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	v, isAdmin, err := resolveVote(key)
	if err != nil {
		return sendInternal(c, err)
	}
	// The route exists for both link shapes; only the admin link may use
	// it, so a user key looks up like a missing link.
	if v == nil || !isAdmin {
		return notFoundHandler(c)
	}
	if v.Closed || voteEnded(v, time.Now()) {
		return c.Status(410).SendString("This vote has ended")
	}
	id := strings.TrimSpace(c.FormValue("respondentid"))
	if findVoteAnswer(v.Answers, id) == nil {
		return notFoundHandler(c)
	}
	name, msg := validName(c.FormValue("name"))
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	marks, notes, msg := collectVoteAnswers(c, v)
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	if _, err := saveVote(key, true, func(vt *database.Vote) {
		vt.Answers = upsertVoteAnswer(vt.Answers, id, name, marks, notes, time.Now())
	}); err != nil {
		return voteSaveError(c, err)
	}
	return c.Redirect().To("/vote/" + key)
}

// voteAnswerDeletePostHandler deletes an answer: via the admin link any
// voter's; via the user link only the own (identity cookie) answer, which
// also expires the identity cookie.
func voteAnswerDeletePostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	v, isAdmin, err := resolveVote(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if v == nil {
		return notFoundHandler(c)
	}
	if v.Closed || voteEnded(v, time.Now()) {
		return c.Status(410).SendString("This vote has ended")
	}
	id := strings.TrimSpace(c.FormValue("respondentid"))
	if id == "" {
		return c.Status(400).SendString("Missing respondent")
	}
	identity := c.Cookies(voteCookieName(v))
	if !isAdmin && id != identity {
		// A user link may only ever act on the own identity.
		return notFoundHandler(c)
	}
	if findVoteAnswer(v.Answers, id) == nil {
		return notFoundHandler(c)
	}
	if !isAdmin && !verifyCSRF(c, identity) {
		return c.Status(401).SendString("Unknown or stale identity — reload the page and try again")
	}
	if _, err := saveVote(key, isAdmin, func(vt *database.Vote) {
		vt.Answers, _ = removeVoteAnswer(vt.Answers, id)
	}); err != nil {
		return voteSaveError(c, err)
	}
	if !isAdmin {
		dropVoteIdentityCookie(c, v)
	}
	return c.Redirect().To("/vote/" + key)
}

// ---- vote lifecycle (admin link only) ----

// voteClosePollHandler closes a running vote: it stops accepting answers
// until the admin re-opens it, while both links keep rendering the answers
// table.
func voteClosePollHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	v, isAdmin, err := resolveVote(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if v == nil || !isAdmin {
		return notFoundHandler(c)
	}
	if voteEnded(v, time.Now()) {
		return c.Status(410).SendString("This vote has ended")
	}
	if _, err := saveVote(key, true, func(vt *database.Vote) {
		vt.Closed = true
	}); err != nil {
		return voteSaveError(c, err)
	}
	return c.Redirect().To("/vote/" + key)
}

// voteReopenPollHandler re-opens a closed vote: new answers can be
// submitted and existing ones edited or deleted again.
func voteReopenPollHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	v, isAdmin, err := resolveVote(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if v == nil || !isAdmin {
		return notFoundHandler(c)
	}
	if voteEnded(v, time.Now()) {
		return c.Status(410).SendString("This vote has ended")
	}
	if _, err := saveVote(key, true, func(vt *database.Vote) {
		vt.Closed = false
	}); err != nil {
		return voteSaveError(c, err)
	}
	return c.Redirect().To("/vote/" + key)
}

// voteDeletePollHandler purges the vote (admin link only): its row with
// every answer, and with it each voter's stored identity. The browser-side
// identity cookie is dropped in the redirect, where it goes inert.
func voteDeletePollHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	v, isAdmin, err := resolveVote(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if v == nil || !isAdmin {
		return notFoundHandler(c)
	}
	if err := db.DeleteVote(v.Key); err != nil {
		return sendInternal(c, err)
	}
	dropVoteIdentityCookie(c, v)
	return c.Redirect().To("/")
}

// deletePollHandler purges the poll (admin link only): its row with every
// answer, and with it each respondent's stored identity. The browser-side
// identity cookie is dropped in the redirect, where it goes inert.
func deletePollHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	m, isAdmin, err := resolveMeeting(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if m == nil || !isAdmin {
		return notFoundHandler(c)
	}
	if err := db.Delete(m.Key); err != nil {
		return sendInternal(c, err)
	}
	dropIdentityCookie(c, m)
	return c.Redirect().To("/")
}
