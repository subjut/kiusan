package main

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// ---- vote identity cookie ----

// voteCookieName builds the per-vote identity cookie name from the user
// link key. Both links share one identity this way, and the vote's cookie
// can be dropped (answer deleted) without touching other votes' cookies.
func voteCookieName(v *database.Vote) string {
	return "kiusan-" + v.Key
}

// voteEnded reports whether the vote has reached its end.
func voteEnded(v *database.Vote, now time.Time) bool {
	return now.After(v.Expires)
}

// ---- vote answer lists ----

// findVoteAnswer returns the answer stored under the identity token, if any.
func findVoteAnswer(list database.VoteAnswerList, id string) *database.VoteAnswer {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

// upsertVoteAnswer replaces or appends the answer stored under the identity
// token, so editing an answer keeps the entry.
func upsertVoteAnswer(list database.VoteAnswerList, id, name string, marks, notes map[string]string, now time.Time) database.VoteAnswerList {
	for i := range list {
		if list[i].ID == id {
			list[i].Name = name
			list[i].Marks = marks
			list[i].Notes = notes
			list[i].UpdatedAt = now
			return list
		}
	}
	return append(list, database.VoteAnswer{ID: id, Name: name, Marks: marks, Notes: notes, UpdatedAt: now})
}

// removeVoteAnswer drops the answer stored under the identity token.
func removeVoteAnswer(list database.VoteAnswerList, id string) (database.VoteAnswerList, bool) {
	for i := range list {
		if list[i].ID == id {
			return append(list[:i], list[i+1:]...), true
		}
	}
	return list, false
}

// validVoteMark reports whether val is a mark the vote's type accepts:
// an unmarked (neutral) option in every type, "y" in every mark type,
// "n" in yes-no and yes-maybe-no, "m" in yes-maybe-no only.
func validVoteMark(vt, val string) bool {
	switch val {
	case "":
		return true
	case "y":
		return true
	case "n":
		return vt == database.VoteTypeYesNo || vt == database.VoteTypeYesMaybeNo
	case "m":
		return vt == database.VoteTypeYesMaybeNo
	}
	return false
}

// collectVoteAnswers reads the per-choice answer fields of a submission:
// mark mode reads a<choice ID> (only the marks the vote's type accepts,
// see validVoteMark), free-text mode reads n<choice ID> (plain text) —
// answers key off the option's stable ID, so they stay with the option
// no matter how it is reordered. A must-answer vote rejects a submission
// with an unanswered option; a single-choice vote rejects a second yes
// (mark mode) or a second filled note (free-text mode) — the
// client-side gating is UI, this is the tamper guard. An unmarked
// option stays neutral in every type.
func collectVoteAnswers(c fiber.Ctx, v *database.Vote) (marks, notes map[string]string, msg string) {
	if v.VoteType == database.VoteTypeFreeText {
		notes = make(map[string]string, len(v.Choices))
		nonEmpty := 0
		for i := range v.Choices {
			id := v.Choices[i].ID
			txt := strings.TrimSpace(c.FormValue("n" + id))
			if utf8.RuneCountInString(txt) > maxVoteNoteLen {
				return nil, nil, "Note too long (max 150 characters)"
			}
			if txt != "" {
				nonEmpty++
				notes[id] = txt
			}
		}
		if v.SingleChoice && nonEmpty > 1 {
			return nil, nil, "This vote allows choosing only one option"
		}
		if v.MustAnswer && nonEmpty != len(v.Choices) {
			return nil, nil, "Please answer every option"
		}
		return nil, notes, ""
	}
	marks = make(map[string]string, len(v.Choices))
	yes := 0
	for i := range v.Choices {
		id := v.Choices[i].ID
		val := c.FormValue("a" + id)
		if !validVoteMark(v.VoteType, val) {
			return nil, nil, "Invalid answer option"
		}
		if v.MustAnswer && val == "" {
			return nil, nil, "Please answer every option"
		}
		if val == "y" {
			yes++
		}
		if val != "" {
			marks[id] = val
		}
	}
	if v.SingleChoice && yes > 1 {
		return nil, nil, "This vote allows choosing only one option"
	}
	return marks, nil, ""
}

// ---- vote load/save with compare-and-set ----

// loadVote fetches the vote by user key (byAdmin=false) or admin key.
func loadVote(key string, byAdmin bool) (*database.Vote, error) {
	if byAdmin {
		return db.GetVoteAdmin(key)
	}
	return db.GetVote(key)
}

// saveVote is the vote's compare-and-set save.
func saveVote(key string, byAdmin bool, mutate func(v *database.Vote)) (*database.Vote, error) {
	return saveEntity(loadVote, db.UpdateVote, key, byAdmin, mutate)
}

// choiceRow is one option row of a vote form submission: the option's
// text and, on edit, the ID the row was rendered with ("" on create).
type choiceRow struct {
	text  string
	oldID string
}

// assignChoiceIDs mints the stable IDs of a vote's options: a row keeps
// its ID only while its text is unchanged from the stored vote — a
// changed text is a new option (a fresh ID, so the old option's answers
// are pruned by pruneVoteAnswers), a deleted row drops its ID, and a new
// row mints one. Reordering carries every ID, so the answers travel with
// their option.
func assignChoiceIDs(old database.ChoiceList, rows []choiceRow) (database.ChoiceList, error) {
	kept := make(map[string]string, len(old))
	for i := range old {
		kept[old[i].ID] = old[i].Text
	}
	taken := make(map[string]struct{}, len(rows))
	choices := make(database.ChoiceList, 0, len(rows))
	for i := range rows {
		id := ""
		if oldID := rows[i].oldID; oldID != "" && kept[oldID] == rows[i].text {
			if _, dup := taken[oldID]; !dup {
				id = oldID
			}
		}
		if id == "" {
			for {
				fresh, err := store.RandomKey(12)
				if err != nil {
					return nil, err
				}
				if _, dup := taken[fresh]; !dup {
					id = fresh
					break
				}
			}
		}
		taken[id] = struct{}{}
		choices = append(choices, database.Choice{ID: id, Text: rows[i].text})
	}
	return choices, nil
}

// pruneVoteAnswers drops stored marks and notes for choices the vote no
// longer offers — the keys that no longer match a choice ID — and the
// whole map the vote's type no longer uses (an edit that flips the type),
// plus marks the new type doesn't offer ("n"/"m" of a vote that is now
// yes, "m" of a vote that is now yes-no), called when the vote is edited.
func pruneVoteAnswers(v *database.Vote) {
	valid := make(map[string]struct{}, len(v.Choices))
	for i := range v.Choices {
		valid[v.Choices[i].ID] = struct{}{}
	}
	for i := range v.Answers {
		for k := range v.Answers[i].Marks {
			if _, ok := valid[k]; !ok {
				delete(v.Answers[i].Marks, k)
			}
		}
		for k := range v.Answers[i].Notes {
			if _, ok := valid[k]; !ok {
				delete(v.Answers[i].Notes, k)
			}
		}
	}
	if v.VoteType == database.VoteTypeFreeText {
		for i := range v.Answers {
			v.Answers[i].Marks = nil
		}
	} else {
		for i := range v.Answers {
			v.Answers[i].Notes = nil
			for k, mk := range v.Answers[i].Marks {
				switch v.VoteType {
				case database.VoteTypeYes:
					if mk == "n" || mk == "m" {
						delete(v.Answers[i].Marks, k)
					}
				case database.VoteTypeYesNo:
					if mk == "m" {
						delete(v.Answers[i].Marks, k)
					}
				}
			}
		}
	}
}

// resolveVote finds the vote behind a link key and reports which view the
// key matched: the user key renders the respondent page, the admin key
// the admin page. A missing key yields a nil vote, not an error.
func resolveVote(key string) (*database.Vote, bool, error) {
	v, err := db.GetVote(key)
	if err == nil {
		return v, false, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, false, err
	}
	av, err := db.GetVoteAdmin(key)
	if err == nil {
		return av, true, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}
