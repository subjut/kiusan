package main

import (
	"errors"
	"log"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"
	"golang.org/x/text/language"
)

// pageData is the binding every page renders through the home layout. A
// typed struct (not a fiber.Map) so the templates can call the T method
// with arguments — text/template only lets FuncMap functions and methods
// take arguments, not function values in a data map. c stays unexported:
// it feeds T, the templates never see it. Exactly one of the page
// pointers is set per render; the templates reach the page's data
// through it ({{.Meet.Tz}}, {{.Vote.ModeKey}}, {{.MeetForm.Timezones}},
// {{.VoteForm.Choices}}).
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

	// The page's own data (defined in the page's file).
	Meet     *meetingPage
	Vote     *votePage
	MeetForm *meetingForm
	VoteForm *voteForm
}

// formBase is the shared upper section of the create/edit forms (title,
// contact, description, hide-users, end date); both form pages embed it,
// and the templates reach its fields promoted ({{.MeetForm.Title}}).
type formBase struct {
	Title        string
	Email        string
	Description  string
	HideUsers    bool
	Expiry       string
	DefaultExp   string
	MinExp       string
	FormTitleKey string
	SubmitLabel  string
}

// answerPage is the data shared by the meeting and vote pages: the
// header meta, the admin links section, the answer form row, and the
// table's aria strings. The pages embed it, and the shared partials
// receive it as their data — c stays unexported, it feeds T.
type answerPage struct {
	c fiber.Ctx

	Title             string
	Description       string
	Email             string
	Ends              string
	EndsKey           string
	Ended             bool
	Closed            bool
	IsAdmin           bool
	UserLink          string
	AdminLink         string
	EditLink          string
	CloseAction       string
	ReopenAction      string
	DeletePollAction  string
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

// T translates one catalog key in the viewer's language; the partials
// that receive an answerPage call it as {{.T "key"}}.
func (p *answerPage) T(id string) string {
	return translator.MustLocalize(p.c, id)
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

// pressAttr renders a boolean as the "true" / "false" string the mark
// buttons' aria-pressed / disabled attributes take in the templates.
func pressAttr(on bool) string {
	if on {
		return "true"
	}
	return "false"
}

// homeHandler renders the landing page.
func homeHandler(c fiber.Ctx) error {
	return renderPage(c, "index", "pagetitle.home", "", &pageData{})
}

// ---- small response helpers ----

func notFoundHandler(c fiber.Ctx) error {
	return c.SendStatus(404)
}

// linkGone renders the 404 for a link that used to be a poll or vote (or
// never was): a full page with its own message, distinct from the
// plain-text 404 of an unknown path. c.Render only sets the body and
// content type, so the status set here is the one sent.
func linkGone(c fiber.Ctx) error {
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

// saveError maps the saveMeeting / saveVote outcomes onto responses.
func saveError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, errNotFound):
		return notFoundHandler(c)
	case errors.Is(err, errConflict):
		return sendErr(c, 500, "Too many concurrent edits, try again")
	}
	return sendInternal(c, err)
}

// ---- shared form plumbing ----

// Form size limits: they bound what one submission can store and render,
// so a single poll can't bloat the database or its page.
const (
	maxCandidateSlots = 100
	maxTitleLen       = 100
	maxEmailLen       = 256
	maxDescriptionLen = 5000
	maxChoices        = 20
	maxChoiceLen      = 100
	maxVoteNoteLen    = 150
)

// validateTextFields caps the free-text fields every create/edit form
// shares.
func validateTextFields(title, email, description string) string {
	switch {
	case utf8.RuneCountInString(title) > maxTitleLen:
		return "Title is too long (max 100 characters)"
	case utf8.RuneCountInString(email) > maxEmailLen:
		return "Email is too long (max 256 characters)"
	case utf8.RuneCountInString(description) > maxDescriptionLen:
		return "Description is too long (max 5000 characters)"
	}
	return ""
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
