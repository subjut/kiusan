package main

import (
	"strings"
	"testing"
	"time"

	"github.com/subjut/kiusan/database"
)

// The .ics builder and download-name rules are pure functions: exact output
// on a fixed instant, the UTC and all-day shapes, chronological order and
// determinism, and the RFC-5545 line folding and text escaping.

func TestBuildMeetingICSExact(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	m := &database.Meeting{
		Key:         "abc123",
		Title:       "Sync",
		Description: "Agenda; items",
		TimeZone:    "Europe/Berlin",
	}
	now := time.Date(2026, 11, 26, 9, 15, 0, 0, time.UTC)
	slot := time.Date(2027, 1, 1, 8, 0, 0, 0, berlin)
	want := "BEGIN:VCALENDAR\r\n" +
		"VERSION:2.0\r\n" +
		"PRODID:-//kiusan//Golang ICS Library\r\n" +
		"BEGIN:VEVENT\r\n" +
		"UID:kiusan/abc123/" + slotKey(slot) + "\r\n" +
		"DTSTAMP:20261126T091500Z\r\n" +
		"SUMMARY:Sync\r\n" +
		"DESCRIPTION:Agenda\\; items\r\n" +
		"DTSTART;TZID=Europe/Berlin:20270101T080000\r\n" +
		"DTEND;TZID=Europe/Berlin:20270101T080000\r\n" +
		"END:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	if got := buildMeetingICS(m, []time.Time{slot}, now); got != want {
		t.Fatalf("calendar:\n%q\nwant:\n%q", got, want)
	}
}

// A poll without a timezone renders UTC instants with the Z suffix and no
// TZID parameter at all.
func TestBuildMeetingICSUtc(t *testing.T) {
	m := &database.Meeting{Key: "k", Title: "UTC poll", TimeZone: ""}
	now := time.Date(2026, 11, 26, 9, 15, 0, 0, time.UTC)
	slot := time.Date(2027, 1, 1, 8, 0, 0, 0, time.UTC)
	got := buildMeetingICS(m, []time.Time{slot}, now)
	for _, want := range []string{
		"DTSTART:20270101T080000Z",
		"DTEND:20270101T080000Z",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("calendar missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "TZID") {
		t.Fatalf("a UTC poll must carry no TZID parameter:\n%s", got)
	}
}

// A midnight slot in the poll's zone is a zero-length all-day event (same
// date for start and end, VALUE=DATE).
func TestBuildMeetingICSAllDay(t *testing.T) {
	m := &database.Meeting{Key: "k", Title: "All day", TimeZone: "Europe/Berlin"}
	now := time.Date(2026, 11, 26, 9, 15, 0, 0, time.UTC)
	// Berlin 2026-12-01 00:00.
	slot := time.Date(2026, 11, 30, 23, 0, 0, 0, time.UTC)
	got := buildMeetingICS(m, []time.Time{slot}, now)
	for _, want := range []string{
		"DTSTART;VALUE=DATE:20261201",
		"DTEND;VALUE=DATE:20261201",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("calendar missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "TZID") {
		t.Fatalf("an all-day event must carry no TZID parameter:\n%s", got)
	}
}

// Events come out chronologically regardless of input order, the caller's
// slice is not reordered, and a build is byte-stable for the same inputs.
func TestBuildMeetingICSOrderAndStability(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	m := &database.Meeting{Key: "k", Title: "Two", TimeZone: "Europe/Berlin"}
	now := time.Date(2026, 11, 26, 9, 15, 0, 0, time.UTC)
	early := time.Date(2027, 2, 1, 10, 0, 0, 0, berlin)
	late := time.Date(2027, 2, 2, 10, 0, 0, 0, berlin)
	slots := []time.Time{late, early}

	first := buildMeetingICS(m, slots, now)
	second := buildMeetingICS(m, slots, now)
	if first != second {
		t.Fatalf("two builds of the same inputs must be identical:\n%s\n%s", first, second)
	}
	iEarly := strings.Index(first, "20270201T100000")
	if iEarly < 0 {
		t.Fatalf("calendar missing the early event:\n%s", first)
	}
	iLate := strings.Index(first, "20270202T100000")
	if iLate < 0 {
		t.Fatalf("calendar missing the late event:\n%s", first)
	}
	if iEarly > iLate {
		t.Fatalf("events must be chronological:\n%s", first)
	}
	if !slots[0].Equal(late) || !slots[1].Equal(early) {
		t.Fatalf("the caller's slot slice must not be reordered: %v", slots)
	}
}

// Long values fold at 75 octets with a space continuation; commas,
// semicolons and line breaks are text-escaped.
func TestBuildMeetingICSFoldAndEscape(t *testing.T) {
	berlin := mustZone(t, "Europe/Berlin")
	m := &database.Meeting{
		Key:         "k",
		Title:       strings.Repeat("a", 100),
		Description: "A, b; c\nsecond line",
		TimeZone:    "Europe/Berlin",
	}
	now := time.Date(2026, 11, 26, 9, 15, 0, 0, time.UTC)
	slot := time.Date(2027, 1, 1, 8, 0, 0, 0, berlin)
	got := buildMeetingICS(m, []time.Time{slot}, now)

	folded := false
	for i, line := range strings.Split(got, "\r\n") {
		if len(line) > 75 {
			t.Fatalf("line %d exceeds 75 octets (%d):\n%s", i, len(line), line)
		}
		if i > 0 && strings.HasPrefix(line, " ") {
			folded = true
		}
	}
	if !folded {
		t.Fatalf("the 100-char title must fold onto a continuation line:\n%s", got)
	}
	for _, want := range []string{
		"A\\, b\\; c\\nsecond line",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("calendar missing escaped description %q:\n%s", want, got)
		}
	}
}

func TestIcsFilename(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"Team; Stand-up", "Team-Stand-up.ics"},
		{"hello -  world.v1", "hello-world-v1.ics"},
		{"  trimmed  ", "trimmed.ics"},
		{"Übung", "bung.ics"},
		{"ÄÖÜ äöü", "meeting.ics"},
		{"", "meeting.ics"},
		{strings.Repeat("x", 60), strings.Repeat("x", 50) + ".ics"},
	} {
		if got := icsFilename(c.in); got != c.want {
			t.Errorf("icsFilename(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
