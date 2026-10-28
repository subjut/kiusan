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
	"time"

	// Bundle Go's zoneinfo so time.LoadLocation works on hosts without
	// system tzdata (Alpine, minimal containers).
	_ "time/tzdata"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/favicon"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/static"
	"github.com/gofiber/template/html/v3"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// db is the storage backend for the whole app. Handlers talk to storage
// only through it; tests swap it for a throwaway instance per run.
var db store.MeetingStore

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
	// Refuse to render inside any frame (clickjacking).
	app.Use(func(c fiber.Ctx) error {
		c.Set("X-Frame-Options", "DENY")
		c.Set("Content-Security-Policy", "frame-ancestors 'none'")
		return c.Next()
	})

	// Make sure syle files and favicons are available
	// (served from the site root; a file miss resets the response and calls Next)
	app.Get("/*", static.New(filepath.Join(staticDir, "css")))
	app.Get("/*", static.New(filepath.Join(staticDir, "js")))
	app.Get("/*", static.New(filepath.Join(staticDir, "favicon")))

	// Normal routes
	app.Get("/", homeHandler)
	app.Get("/1-1-form", formHandler)
	app.Post("/1-1-form", formPostHandler)
	// The edit form is reachable by the admin link only.
	app.Get("/1-1-form/edit/+", formEditHandler)

	// Meeting page routes; the gobbler takes either link key of the poll.
	app.Get("/meet/+", meetingHandler)
	app.Post("/meet/+/answer", answerPostHandler)
	app.Post("/meet/+/answer/edit", answerEditPostHandler)
	app.Post("/meet/+/answer/delete", answerDeletePostHandler)

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

	// For debugging, set to false or comment for production
	// Reload the templates on each render, good for development
	engine.Reload(true)

	// Debug will print each template that is parsed, good for debugging
	engine.Debug(true)

	// Create app
	app := fiber.New(fiber.Config{
		Views:       engine,
		ReadTimeout: 10 * time.Second, // Don't keep cons alive for too long
	})

	// Serve favicon.ico from memory instead of disk
	app.Use(favicon.New(favicon.Config{
		File: filepath.Join(staticDir, "favicon", "favicon.ico"),
	}))

	// Logging middleware
	app.Use(logger.New())

	// Set up all routes
	initRoutes(app, staticDir)

	return app
}

func main() {
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
	signal.Notify(quit, os.Interrupt)

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

func homeHandler(c fiber.Ctx) error {
	return c.Render("index", fiber.Map{
		"pagetitle": "kiusan | Hi!",
	}, "layouts/home")
}

// ---- 1-1 form: create & edit ----

func formHandler(c fiber.Ctx) error {
	now := time.Now()
	return c.Render("1-1-form", fiber.Map{
		"pagetitle":    "kiusan | Meeting Form!",
		"timezones":    timezoneList,
		"meetingkey":   "",
		"title":        "",
		"email":        "",
		"description":  "",
		"timezone":     "",
		"extraTz":      "",
		"hideusers":    false,
		"singlesignup": false,
		"datesAttr":    "",
		"timesAttr":    "",
		"expiry":       "",
		"defaultExp":   now.UTC().AddDate(0, 3, 0).Format("2006-01-02"),
		"minExp":       now.UTC().Format("2006-01-02"),
	}, "layouts/home")
}

// formEditHandler renders the 1-1 form prefilled with the poll's stored
// entries (admin link only), so editing a poll keeps both of its links.
func formEditHandler(c fiber.Ctx) error {
	m, err := db.GetAdmin(c.Params("+"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return notFoundHandler(c)
		}
		return c.Status(500).SendString(err.Error())
	}

	// The picker state the poll was created with: its slots split into
	// unique dates and times in the meeting's zone. Midnight slots only
	// become a 00:00 time when some slot carries a real time, keeping the
	// re-edit equivalent for date-only polls.
	loc, _ := time.LoadLocation(m.TimeZone)
	hasTime := false
	for _, d := range m.Dates {
		t := d.In(loc)
		if t.Hour() != 0 || t.Minute() != 0 {
			hasTime = true
			break
		}
	}
	dates := map[string]struct{}{}
	times := map[string]struct{}{}
	for _, d := range m.Dates {
		t := d.In(loc)
		dates[t.Format("2006-01-02")] = struct{}{}
		if hasTime {
			times[t.Format("15:04")] = struct{}{}
		}
	}
	joined := func(set map[string]struct{}) string {
		list := make([]string, 0, len(set))
		for v := range set {
			list = append(list, v)
		}
		sort.Strings(list)
		return strings.Join(list, ",")
	}

	expiry := ""
	if m.Expires != nil {
		expiry = m.Expires.In(loc).Format("2006-01-02")
	}

	return c.Render("1-1-form", fiber.Map{
		"pagetitle":    "kiusan | Edit: " + m.Title,
		"timezones":    timezoneList,
		"meetingkey":   m.AdminKey,
		"title":        m.Title,
		"email":        m.Email,
		"description":  m.Description,
		"timezone":     m.TimeZone,
		"extraTz":      extraTimezone(m.TimeZone),
		"hideusers":    m.HideUsers,
		"singlesignup": m.SingleSignup,
		"datesAttr":    joined(dates),
		"timesAttr":    joined(times),
		"expiry":       expiry,
		"defaultExp":   "",
		"minExp":       time.Now().UTC().Format("2006-01-02"),
	}, "layouts/home")
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
		return c.Status(400).SendString(err.Error())
	}

	// The two poll options; read explicitly so the checkbox values never
	// have to satisfy the struct binder.
	form.HideUsers = c.FormValue("hide-users") != ""
	form.SingleSignup = c.FormValue("single-signup") != ""

	// Timezone the meeting's dates/times are authored and displayed in;
	// optional, empty (legacy) renders as UTC.
	form.TimeZone = strings.TrimSpace(c.FormValue("timezone"))
	loc, err := time.LoadLocation(form.TimeZone)
	if err != nil {
		return c.Status(400).SendString("Invalid timezone: " + form.TimeZone)
	}

	// Dates
	var dates []time.Time
	for i := 0; true; i++ {
		date := c.FormValue("date" + strconv.Itoa(i))
		if date == "" {
			break
		}

		// HTML date inputs submit YYYY-MM-DD
		newDate, err := time.Parse("2006-01-02", date)
		if err != nil {
			return c.Status(400).SendString("Invalid date: " + date)
		}
		dates = append(dates, newDate)
	}

	// Times, each propagated to every selected date.
	var times []time.Time
	for i := 0; true; i++ {
		t := c.FormValue("time" + strconv.Itoa(i))
		if t == "" {
			break
		}
		newTime, err := time.Parse("15:04", t)
		if err != nil {
			return c.Status(400).SendString("Invalid time: " + t)
		}
		times = append(times, newTime)
	}

	//// Feed form into database
	// Candidate slots = dates × times; without times, dates stay at
	// midnight (behavior pinned by the form test).
	for _, d := range dates {
		if len(times) == 0 {
			form.Dates = append(form.Dates, d)
			continue
		}
		for _, t := range times {
			form.Dates = append(form.Dates,
				time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), 0, 0, loc))
		}
	}

	// End date (form field "enddate" — deliberately not named after
	// Meeting.Expires, whose binding the date-only value could not feed).
	// Empty means the 3-month default.
	endDate, msg := parseEndDate(c.FormValue("enddate"), time.Now())
	if msg != "" {
		return c.Status(400).SendString(msg)
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
		return c.Status(500).SendString(err.Error())
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
		return time.Time{}, "Invalid end date: " + raw
	}
	today := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC)
	if d.Before(today) {
		return time.Time{}, "End date is in the past"
	}
	return d, ""
}

// endOf turns a calendar end date into the instant the poll stops
// accepting answers: the end of that day in the meeting's zone.
func endOf(d time.Time, loc *time.Location) *time.Time {
	t := time.Date(d.Year(), d.Month(), d.Day(), 23, 59, 59, 0, loc)
	return &t
}

// meetingSaveError maps the saveMeeting outcomes onto responses.
func meetingSaveError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errNotFoundPoll):
		return notFoundHandler(c)
	case errors.Is(err, errConflict):
		return c.Status(500).SendString("Too many concurrent edits, try again")
	}
	return c.Status(500).SendString(err.Error())
}

// ---- poll page ----

// meetingHandler renders the poll page for a user or admin link: the same
// page, with the admin link adding the copyable links, the edit-poll
// button and the per-row edit/delete icons on every answer.
func meetingHandler(c fiber.Ctx) error {
	m, isAdmin, err := resolveMeeting(c.Params("+"))
	if err != nil {
		return c.Status(500).SendString(err.Error())
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
	loc, _ := time.LoadLocation(m.TimeZone)
	tzName := m.TimeZone
	if tzName == "" {
		tzName = "UTC"
	}
	ends := ""
	if m.Expires != nil {
		ends = m.Expires.In(loc).Format("2006-01-02")
	}

	// Table column slots, prefilled with the form target's marks.
	type formSlot struct {
		Key, Label string
		PressR     string
	}
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
	slots := make([]formSlot, 0, len(m.Dates))
	for _, d := range m.Dates {
		status := ""
		if formTarget != nil {
			status = formTarget.Marks[slotKey(d)]
		}
		slots = append(slots, formSlot{Key: slotKey(d), Label: d.In(loc).Format("Mon 2006-01-02 15:04"),
			PressR: press(status == "")})
	}

	// The respondents visible on this page.
	var shown database.AnswerList
	switch {
	case isAdmin || !m.HideUsers:
		shown = m.Answers
	case own != nil:
		shown = database.AnswerList{*own}
	}
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
	cols := make([]respCol, 0, len(shown))
	for i := range shown {
		marks := make([]string, len(m.Dates))
		syms := make([]string, len(m.Dates))
		csls := make([]string, len(m.Dates))
		pg := make([]string, len(m.Dates))
		py := make([]string, len(m.Dates))
		pr := make([]string, len(m.Dates))
		for j, d := range m.Dates {
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
	data := fiber.Map{
		"pagetitle":        "kiusan | " + m.Title,
		"title":            m.Title,
		"description":      m.Description,
		"email":            m.Email,
		"tzname":           tzName,
		"ends":             ends,
		"ended":            ended,
		"isadmin":          isAdmin,
		"userlink":         absURL(c, "/meet/"+m.Key),
		"adminlink":        absURL(c, "/meet/"+m.AdminKey),
		"editlink":         "/1-1-form/edit/" + m.AdminKey,
		"single":           m.SingleSignup,
		"slots":            slots,
		"cols":             cols,
		"selflink":         "/meet/" + viewKey,
		"formnew":          formNew,
		"formid":           formID,
		"formname":         formName,
		"formaction":       formAction,
		"formrespondentid": formRespondentID,
		"deleteaction":     "/meet/" + viewKey + "/answer/delete",
		// CSRF travels with the forms that post through the cookie
		// identity: the form row (unless it posts through the capability
		// admin edit endpoint) and, in the user view, the delete form.
		"answercsrf": csrf != "" && formRespondentID == "" && (formNew || formID != ""),
		"delcsrf":    csrf != "" && !isAdmin,
		"csrftoken":  csrf,
	}
	return c.Render("meet", data, "layouts/home")
}

// absURL renders a site-absolute URL for the copyable link boxes on the
// admin page.
func absURL(c fiber.Ctx, path string) string {
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
		return c.Status(500).SendString(err.Error())
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
		return c.Status(500).SendString(err.Error())
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
		return c.Status(500).SendString(err.Error())
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
