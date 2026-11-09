package main

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// ---- meeting form: create & edit ----

// meetingForm is the meeting create/edit form's page data (the picker
// and timezone fields on top of the shared form base).
type meetingForm struct {
	formBase
	Timezones    []string
	MeetingKey   string
	Timezone     string
	ExtraTz      string
	SingleSignup bool
	SlotsAttr    string
	CalPrev      string
	CalNext      string
}

func meetFormHandler(c fiber.Ctx) error {
	now := time.Now()
	return renderPage(c, "meet-form", "pagetitle.form", "", &pageData{
		MeetForm: &meetingForm{
			formBase: formBase{
				FormTitleKey: "form.createTitle",
				DefaultExp:   now.UTC().AddDate(0, 3, 0).Format("2006-01-02"),
				MinExp:       now.UTC().Format("2006-01-02"),
				SubmitLabel:  translator.MustLocalize(c, "form.submit"),
			},
			Timezones: timezoneList,
			// Attribute-context strings, precomputed: a literal inside a
			// {{...}} action in an attribute value breaks html/template's
			// tokenization.
			CalPrev: translator.MustLocalize(c, "form.calPrev"),
			CalNext: translator.MustLocalize(c, "form.calNext"),
		},
	})
}

// meetFormEditHandler renders the meeting form prefilled with the poll's
// stored entries (admin link only), so editing a poll keeps both of its
// links.
func meetFormEditHandler(c fiber.Ctx) error {
	m, err := db.GetAdmin(c.Params("+"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// The admin link of a deleted poll renders the gone page.
			return linkGone(c)
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

	return renderPage(c, "meet-form", "pagetitle.formEdit", m.Title, &pageData{
		MeetForm: &meetingForm{
			formBase: formBase{
				Title:        m.Title,
				Email:        m.Email,
				Description:  m.Description,
				HideUsers:    m.HideUsers,
				FormTitleKey: "form.editTitle",
				Expiry:       expiry,
				MinExp:       time.Now().UTC().Format("2006-01-02"),
				SubmitLabel:  translator.MustLocalize(c, "form.saveChanges"),
			},
			Timezones:    timezoneList,
			MeetingKey:   m.AdminKey,
			Timezone:     m.TimeZone,
			ExtraTz:      extraTimezone(m.TimeZone),
			SingleSignup: m.SingleSignup,
			SlotsAttr:    strings.Join(slots, ","),
			// Attribute-context strings, precomputed (see meetFormHandler).
			CalPrev: translator.MustLocalize(c, "form.calPrev"),
			CalNext: translator.MustLocalize(c, "form.calNext"),
		},
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

// meetFormPostHandler creates a poll, or — when the form carries a
// meetingkey (an admin link key) — updates that poll in place, keeping
// both of its links.
func meetFormPostHandler(c fiber.Ctx) error {
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
	if msg := validateTextFields(form.Title, form.Email, form.Description); msg != "" {
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
			// Enabling single-signup on a poll that already collects answers
			// invalidates every multi-slot answer: wipe all respondents
			// (their stored identities along) so everyone starts from a
			// fresh poll. (A no-op while the poll is already single-signup:
			// single-slot answers stay valid there.) m carries the stored
			// poll — its old mode — while form the submission.
			if form.SingleSignup && !m.SingleSignup {
				m.Answers = nil
			}
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
			return saveError(c, err)
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
