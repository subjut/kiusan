package main

import (
	"sort"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"

	"github.com/subjut/kiusan/database"
)

// buildMeetingICS renders the poll's slots as a VCALENDAR with one VEVENT
// per slot, chronologically: a timed slot as a zero-length event in the
// poll's zone (the app stores no end times — zero length is RFC-5545
// conformant and renders as the start time in the clients), a midnight
// slot as a zero-length all-day event. The UIDs are deterministic (link
// key + slot instant), so a re-download updates the same calendar event
// instead of adding a duplicate. CRLF endings and the 75-octet folding
// come from the library (ics.WithNewLineWindows).
func buildMeetingICS(m *database.Meeting, slots []time.Time, now time.Time) string {
	// A display copy: the caller's slice (e.g. m.FinalSlots) must not be
	// reordered, and the event order is chronological.
	dates := make([]time.Time, len(slots))
	copy(dates, slots)
	sort.Slice(dates, func(i, j int) bool { return dates[i].Before(dates[j]) })

	// All times are wall clock in the poll's zone (UTC when it has none);
	// the server's own zone never enters the document.
	loc, err := time.LoadLocation(m.TimeZone)
	if err != nil {
		loc = time.UTC
	}

	cal := ics.NewCalendarFor("kiusan")
	for _, s := range dates {
		ev := cal.AddEvent("kiusan/" + m.Key + "/" + slotKey(s))
		ev.SetDtStampTime(now)
		ev.SetSummary(m.Title)
		if m.Description != "" {
			ev.SetDescription(m.Description)
		}
		wall := s.In(loc)
		switch {
		case wall.Hour() == 0 && wall.Minute() == 0:
			// A midnight slot is a date-only slot: a zero-length all-day
			// event (same date start and end, VALUE=DATE).
			ev.SetAllDayStartAt(wall)
			ev.SetAllDayEndAt(wall)
		case m.TimeZone == "":
			// UTC: the Z-suffixed UTC value the setter emits is exactly
			// right.
			ev.SetStartAt(s)
			ev.SetEndAt(s)
		default:
			// A TZID parameter requires a local wall-clock value without
			// a Z; SetStartAt would normalize to UTC and produce a
			// malformed line (DTSTART;TZID=...:20270101T070000Z).
			w := wall.Format("20060102T150405")
			ev.AddProperty(ics.ComponentPropertyDtStart, w, ics.WithTZID(m.TimeZone))
			ev.AddProperty(ics.ComponentPropertyDtEnd, w, ics.WithTZID(m.TimeZone))
		}
	}
	return cal.Serialize(ics.WithNewLineWindows)
}

// icsFilename builds the download name: the title's letters and digits,
// spaces/hyphens/dots as single hyphens, capped at 50 chars, falling back
// to "meeting" when nothing survives (a non-ASCII-only title).
func icsFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '.':
			if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
				b.WriteByte('-')
			}
		}
		if b.Len() >= 50 {
			break
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "meeting"
	}
	return s + ".ics"
}
