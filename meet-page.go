package main

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
)

// ---- meeting page ----

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

// meetingPage is the meeting page's page data (the timezone, the table
// header and the answer columns on top of the shared answer page).
type meetingPage struct {
	answerPage
	Tz          string
	TzName      string
	Single      bool
	Slots       []formSlot
	Header      []headerDate
	HeaderYears []headerYear
	HasTime     bool
	Cols        []respCol
	FormCol     respCol
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
		return linkGone(c)
	}

	now := time.Now()
	ended := pollEnded(m, now)
	closed := m.Closed
	identity := ensureIdentityCookie(c, cookieName(m), m.Expires, m.Closed, now)
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
	markSym := func(v string) (sym, cls string) {
		switch v {
		case "g":
			return "✓", "g"
		case "y":
			return "(✓)", "y"
		}
		return "✗", "r"
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
		cell := timeCell{Text: "—"} // —: a date-only slot in a timed poll
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
			pg[j] = pressAttr(v == "g")
			py[j] = pressAttr(v == "y")
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
		Meet: &meetingPage{
			answerPage: answerPage{
				c:           c,
				Title:       m.Title,
				Description: m.Description,
				Email:       m.Email,
				Ends:        ends,
				EndsKey:     endsKey,
				Ended:       ended,
				Closed:      closed,
				IsAdmin:     isAdmin,
				UserLink:    absURL(c, "/meet/"+m.Key),
				AdminLink:   absURL(c, "/meet/"+m.AdminKey),
				EditLink:    "/meet-form/edit/" + m.AdminKey,
				// The lifecycle forms post through the admin link (a
				// capability URL) — they only render in the admin view.
				CloseAction:      "/meet/" + m.AdminKey + "/close",
				ReopenAction:     "/meet/" + m.AdminKey + "/reopen",
				DeletePollAction: "/meet/" + m.AdminKey + "/delete",
				SelfLink:         "/meet/" + viewKey,
				FormNew:          formNew,
				FormID:           formID,
				FormName:         formName,
				FormAction:       formAction,
				FormRespondentID: formRespondentID,
				DeleteAction:     "/meet/" + viewKey + "/answer/delete",
				// CSRF travels with the forms that post through the cookie
				// identity: the form row (unless it posts through the
				// capability admin edit endpoint) and, in the user view, the
				// delete form.
				AnswerCSRF: csrf != "" && formRespondentID == "" && (formNew || formID != ""),
				DelCSRF:    csrf != "" && !isAdmin,
				CSRFToken:  csrf,
				// Attribute-context strings, precomputed (see meetFormHandler).
				CopyTitle:         translator.MustLocalize(c, "meet.copyTitle"),
				TableAria:         translator.MustLocalize(c, "meet.tableAria"),
				NameAria:          translator.MustLocalize(c, "meet.nameAria"),
				EditAnswer:        translator.MustLocalize(c, "meet.editAnswer"),
				DeleteAnswer:      translator.MustLocalize(c, "meet.deleteAnswer"),
				NamePh:            translator.MustLocalize(c, "meet.namePh"),
				SubmitAnswerKey:   submitKey,
				SubmitAnswerLabel: translator.MustLocalize(c, submitKey),
			},
			Tz:          tz,
			TzName:      tzName,
			Single:      m.SingleSignup,
			Slots:       slots,
			Header:      header,
			HeaderYears: headerYears,
			HasTime:     hasTime,
			Cols:        cols,
			FormCol:     formCol,
		},
	})
}
