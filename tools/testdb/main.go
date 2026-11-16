// Command testdb builds kiusan's seed test database (test.db by default):
// meeting and vote answer tables at every defined size cap, so the
// max-size rendering and table behaviour can be inspected live.
//
// It writes the file with the app's own store package (same GORM schema,
// same key minting), so the result is byte-compatible with what the server
// reads. The target file is recreated from scratch on every run.
//
// Usage:
//
//	go run ./tools/testdb [-o test.db] [-base http://localhost:8080]
//
// and start the server on it:
//
//	KIUSAN_DSN=./test.db go run .
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// Size caps mirrored from the form-size constants in render.go (they are in
// the main package and cannot be imported); the seed data is built exactly
// at each cap so the max-size tables render with full-length titles,
// emails, descriptions, slot sets, options and notes.
const (
	maxSlots     = 100 // maxCandidateSlots
	maxChoices   = 20  // maxChoices
	maxTitle     = 100 // maxTitleLen
	maxEmail     = 256 // maxEmailLen
	maxDesc      = 5000
	maxChoiceLen = 100 // maxChoiceLen
	maxNoteLen   = 150 // maxVoteNoteLen
	maxNameLen   = 50  // validName's cap
)

// slotKey mirrors the main package's slot key: the slot's absolute UTC
// instant, seconds stripped — the key stored in every answer's Marks.
func slotKey(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04Z07:00")
}

// padTo extends s to exactly n bytes with trailing dots (all seed strings
// are ASCII, so bytes and runes agree).
func padTo(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(".", n-len(s))
}

// emailAt builds an address of exactly n bytes, so the email field hits its
// cap without becoming a syntax the form would later reject.
func emailAt(n int) string {
	local, domain := "max-size-table-test-", "@kiusan.test"
	return local + strings.Repeat("x", n-len(local)-len(domain)) + domain
}

// futureEnd is the polls' end date: 50 years out, end of that day in the
// poll's zone (the shape parseEndDate produces).
func futureEnd(now time.Time, loc *time.Location) time.Time {
	t := now.AddDate(50, 0, 0).In(loc)
	return time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 0, loc)
}

// respondentNames are the per-poll respondent labels, with every 37th
// hitting the 50-character name cap.
func respondentName(i int) string {
	if i%37 == 0 {
		return padTo("User with a very long name (exactly 50 chars)", maxNameLen)
	}
	return fmt.Sprintf("User %03d", i+1)
}

// seedMeeting stores one meeting with len(slots) slots and voters answers.
// Slot 0 gets a green mark from everybody (all-max totals row) and slot 1
// none (all-zero row, no highlight) so both totals-edge cases show up; the
// rest get a deterministic ~45% green / ~20% yellow mix.
func seedMeeting(db store.Store, title, tz string, slots []time.Time, voters int, rng *rand.Rand, now time.Time) (string, string, error) {
	answers := make(database.AnswerList, 0, voters)
	for v := 0; v < voters; v++ {
		id, err := store.RandomKey(32)
		if err != nil {
			return "", "", err
		}
		marks := make(map[string]string, len(slots))
		for si, s := range slots {
			switch {
			case si == 0:
				marks[slotKey(s)] = "g"
			case si == 1:
				// nobody marks this slot
			default:
				r := rng.Float64()
				if r < 0.45 {
					marks[slotKey(s)] = "g"
				} else if r < 0.65 {
					marks[slotKey(s)] = "y"
				}
			}
		}
		answers = append(answers, database.Answer{
			ID: id, Name: respondentName(v), Marks: marks,
			UpdatedAt: now.Add(-time.Duration(v) * time.Minute),
		})
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", "", err
	}
	m := &database.Meeting{
		Title:       padTo(title, maxTitle),
		Description: padTo("Meeting answer table seeded at every defined size cap (100-char title, 256-char email, 5000-char description, full slot set).", maxDesc),
		Email:       emailAt(maxEmail),
		Dates:       slots,
		TimeZone:    tz,
		Expires:     futureEnd(now, loc),
		Answers:     answers,
	}
	userKey, err := db.Create(m)
	if err != nil {
		return "", "", err
	}
	return userKey, m.AdminKey, nil
}

// seedVote stores one vote with maxChoices options and voters answers,
// shaped to its voting type: y / (m) / n marks by choice ID (y / m in a
// yes / yes-maybe-no, +n where offered, and freetext with notes up to the
// cap), at least one y at the choice (voters may legitimately overlap one
// pick), and one choice nobody answers.
func seedVote(db store.Store, title, vtype string, singleChoice bool, voters int, rng *rand.Rand, now time.Time) (string, string, error) {
	choices := make(database.ChoiceList, 0, maxChoices)
	for i := 0; i < maxChoices; i++ {
		id, err := store.RandomKey(12)
		if err != nil {
			return "", "", err
		}
		text := fmt.Sprintf("Option %02d", i+1)
		if i == 4 {
			// one option label at the 100-character cap
			text = padTo("Option 05 (this label hits the 100-character option cap)", maxChoiceLen)
		}
		choices = append(choices, database.Choice{ID: id, Text: text})
	}

	answers := make(database.VoteAnswerList, 0, voters)
	for v := 0; v < voters; v++ {
		id, err := store.RandomKey(32)
		if err != nil {
			return "", "", err
		}
		va := database.VoteAnswer{ID: id, Name: respondentName(v), UpdatedAt: now.Add(-time.Duration(v) * time.Minute)}
		if singleChoice {
			// one y per voter: most pick option 1 (the totals' max row
			// lands on a single option), the rest spread over options
			// 3 and up (option 2 stays unanswered by design)
			va.Marks = make(map[string]string, 1)
			pick := 0
			if rng.Float64() >= 0.75 {
				pick = 2 + rng.Intn(maxChoices-2)
			}
			va.Marks[choices[pick].ID] = "y"
			answers = append(answers, va)
			continue
		}
		switch vtype {
		case database.VoteTypeFreeText:
			va.Notes = make(map[string]string, maxChoices)
			for ci, ch := range choices {
				if ci == 1 {
					continue // one choice nobody answers
				}
				if v == 0 {
					// one voter with a note at the 150-character cap on every option
					va.Notes[ch.ID] = padTo("A note at the 150-character cap on every option", maxNoteLen)
					continue
				}
				if ci == 2 && v == 1 {
					va.Notes[ch.ID] = padTo("Second voter's one cappy note", maxNoteLen)
					continue
				}
				if rng.Float64() < 0.6 {
					va.Notes[ch.ID] = strings.Repeat(fmt.Sprintf("note %d for option %d: ", v+1, ci+1), rng.Intn(3)+1)
					if len(va.Notes[ch.ID]) > maxNoteLen {
						va.Notes[ch.ID] = va.Notes[ch.ID][:maxNoteLen]
					}
				}
			}
		default:
			va.Marks = make(map[string]string, maxChoices)
			for ci, ch := range choices {
				if ci == 0 {
					va.Marks[ch.ID] = "y" // everybody yes on the first option
				} else if ci != 1 { // option 2: nobody answers it
					r := rng.Float64()
					switch vtype {
					case database.VoteTypeYesNo:
						if r < 0.5 {
							va.Marks[ch.ID] = "y"
						} else if r < 0.75 {
							va.Marks[ch.ID] = "n"
						}
					case database.VoteTypeYesMaybeNo:
						if r < 0.4 {
							va.Marks[ch.ID] = "y"
						} else if r < 0.7 {
							va.Marks[ch.ID] = "m"
						} else if r < 0.85 {
							va.Marks[ch.ID] = "n"
						}
					}
				}
			}
		}
		answers = append(answers, va)
	}

	vote := &database.Vote{
		Title:        title,
		Description:  "Vote answer table seeded at the maximum option count with full-mark distribution.",
		Email:        "max-size-vote@kiusan.test",
		VoteType:     vtype,
		SingleChoice: singleChoice,
		Choices:      choices,
		Expires:      futureEnd(now, time.UTC),
		Answers:      answers,
	}
	userKey, err := db.CreateVote(vote)
	if err != nil {
		return "", "", err
	}
	return userKey, vote.AdminKey, nil
}

// slotSet builds the 100-slot meeting grid: 20 days x 5 times, starting
// three months out, in the poll's zone.
func slotSet(now time.Time, loc *time.Location) []time.Time {
	start := now.In(loc).AddDate(0, 3, 0)
	hours := []int{8, 10, 12, 14, 16}
	slots := make([]time.Time, 0, maxSlots)
	for d := 0; d < 20; d++ {
		day := start.AddDate(0, 0, d)
		for _, h := range hours {
			slots = append(slots, time.Date(day.Year(), day.Month(), day.Day(), h, 0, 0, 0, loc))
		}
	}
	return slots
}

func main() {
	out := flag.String("o", "test.db", "output sqlite file (recreated on every run)")
	base := flag.String("base", "http://localhost:8080", "origin for the printed links")
	flag.Parse()
	baseURL := strings.TrimSuffix(*base, "/")

	if _, err := os.Stat(*out); err == nil {
		log.Printf("removing existing %s", *out)
		if err := os.Remove(*out); err != nil {
			log.Fatalf("remove %s: %v", *out, err)
		}
	}

	db, err := store.Open(store.BackendSQLite, *out)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("close store: %v", err)
		}
	}()

	rng := rand.New(rand.NewSource(42))
	now := time.Now()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		log.Fatalf("load timezone: %v", err)
	}
	slots := slotSet(now, loc)
	// Berlin for the grid's wall clock, the zone stored with the meeting.

	printed := 0
	must := func(what, route string, user, admin string, err error) {
		if err != nil {
			log.Fatalf("%s: %v", what, err)
		}
		printed++
		fmt.Printf("%s\n", what)
		fmt.Printf("  admin: %s/%s/%s\n", baseURL, route, admin)
		fmt.Printf("  user:  %s/%s/%s\n", baseURL, route, user)
	}

	user, admin, err := seedMeeting(db,
		"Max-size meeting: 100 slots x 100 respondents", loc.String(), slots, 100, rng, now)
	must("seed meeting 100x100", "meet", user, admin, err)

	user, admin, err = seedMeeting(db,
		"Max-size meeting: 100 slots x 50 respondents", loc.String(), slots, 50, rng, now)
	must("seed meeting 100x50", "meet", user, admin, err)

	user, admin, err = seedVote(db,
		"Max-size vote: yes/no, 20 options x 40 voters",
		database.VoteTypeYesNo, false, 40, rng, now)
	must("seed vote yes/no", "vote", user, admin, err)

	user, admin, err = seedVote(db,
		"Max-size vote: yes/maybe/no, 20 options x 40 voters",
		database.VoteTypeYesMaybeNo, false, 40, rng, now)
	must("seed vote yes/maybe/no", "vote", user, admin, err)

	user, admin, err = seedVote(db,
		"Max-size vote: free text, 20 options x 30 voters",
		database.VoteTypeFreeText, false, 30, rng, now)
	must("seed vote free text", "vote", user, admin, err)

	user, admin, err = seedVote(db,
		"Max-size vote: yes single-choice, 20 options x 30 voters",
		database.VoteTypeYes, true, 30, rng, now)
	must("seed vote yes single-choice", "vote", user, admin, err)

	fmt.Printf("\nDone: %d tables in %s\n", printed, *out)
}
