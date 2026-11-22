package main

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
)

// ---- vote page ----

// One visible voter's row on a vote: the per-choice marks (mark mode) or
// notes (free-text mode), the per-choice display glyph, and the mark
// buttons' pressed states. An unvoted option stays neutral in every type.
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
	Last     bool // set on the last voter row (the table's last content row when the totals are hidden)
}

// votePage is the vote page's page data (the mode, the type and the
// choice columns on top of the shared answer page).
type votePage struct {
	answerPage
	Single      bool
	MustAnswer  bool
	Choices     database.ChoiceList
	ModeKey     string
	VoteType    string
	VoteCols    []voteCol
	VoteFormCol voteCol
	// Totals: the per-type summary rows (per-choice yes counts, plus
	// maybe / no rows where the type has those marks), rendered as the
	// table's last rows — nil when the totals are hidden for this view
	// (a free-text vote, the hide-totals option, or the user page of a
	// vote with hidden respondents).
	Totals []totalsRow
	// HasFooter: whether the table's borderless footer row renders (the
	// closed admin view's toggle buttons, or the final options' "Final!"
	// labels on both links) — the row above it carries the final frame's
	// bottom edge.
	HasFooter bool
}

// voteHandler renders the vote page for a user or admin link: the same
// page, with the admin link adding the copyable links, the edit-vote
// button and the per-row edit/delete icons on every answer.
func voteHandler(c fiber.Ctx) error {
	v, isAdmin, err := resolveVote(c.Params("+"))
	if err != nil {
		return sendInternal(c, err)
	}
	if v == nil {
		return linkGone(c)
	}

	now := time.Now()
	ended := voteEnded(v, now)
	closed := v.Closed
	identity := ensureIdentityCookie(c, voteCookieName(v), v.Expires, v.Closed, now)
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
	markGlyph := func(mk string) string {
		switch mk {
		case "y":
			return "✓"
		case "m":
			return "(✓)"
		case "n":
			return "✗"
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
	// stored "no" to show), else the mark's glyph.
	displayState := func(a database.VoteAnswer, j int) (string, string) {
		k := v.Choices[j].ID
		if v.VoteType == database.VoteTypeFreeText {
			return a.Notes[k], ""
		}
		return markGlyph(a.Marks[k]), markClass(a.Marks[k])
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
			if v.VoteType == database.VoteTypeFreeText {
				notes[j] = shown[i].Notes[k]
			} else {
				mk := shown[i].Marks[k]
				marks[j] = mk
				if mk != "" {
					fcls[j] = "st-" + markClass(mk)
				}
			}
			s, cl := displayState(shown[i], j)
			syms[j] = s
			csls[j] = cl
			py[j] = pressAttr(marks[j] == "y")
			pm[j] = pressAttr(marks[j] == "m")
			pn[j] = pressAttr(marks[j] == "n")
		}
		cols = append(cols, voteCol{ID: shown[i].ID, Name: shown[i].Name, IsOwn: shown[i].ID == identity,
			Marks: marks, Notes: notes, Syms: syms, Csls: csls, Fcls: fcls,
			PressY: py, PressM: pm, PressN: pn})
	}
	if len(cols) > 0 {
		cols[len(cols)-1].Last = true
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

	// The totals rows: per-choice mark counts, in the type's order (yes
	// first, then maybe / no where the type has those marks), each cell
	// counting the voters who marked that choice (over the stored
	// answers — the form row's in-flight state is not part of it). Hidden
	// on a free-text vote — there are no marks to count — and by the
	// vote's hide-totals option; with hidden respondents the user page
	// loses them, the admin page keeps them.
	var totals []totalsRow
	if v.VoteType != database.VoteTypeFreeText &&
		(isAdmin || (!v.HideUsers && !v.HideTotals)) {
		y, mb, no := make([]int, len(v.Choices)), make([]int, len(v.Choices)), make([]int, len(v.Choices))
		for _, a := range v.Answers {
			for j, c := range v.Choices {
				switch a.Marks[c.ID] {
				case "y":
					y[j]++
				case "m":
					mb[j]++
				case "n":
					no[j]++
				}
			}
		}
		// Each row's max cells take its mark's answer-cell styling
		// (green / amber / red), so the table reads in one language.
		totals = []totalsRow{{NameKey: "totals.yes", Name: translator.MustLocalize(c, "totals.yes"),
			Cls: "tot-g", Mark: "g", Sym: "✓", Values: y, Max: rowMax(y)}}
		if v.VoteType == database.VoteTypeYesMaybeNo {
			totals = append(totals, totalsRow{NameKey: "totals.maybe", Name: translator.MustLocalize(c, "totals.maybe"),
				Cls: "tot-y", Mark: "y", Sym: "(✓)", Values: mb, Max: rowMax(mb)})
		}
		if v.VoteType == database.VoteTypeYesNo || v.VoteType == database.VoteTypeYesMaybeNo {
			totals = append(totals, totalsRow{NameKey: "totals.no", Name: translator.MustLocalize(c, "totals.no"),
				Cls: "tot-r", Mark: "r", Sym: "✗", Values: no, Max: rowMax(no)})
		}
		totals[len(totals)-1].Last = true
	}

	csrf := ""
	if identity != "" && !ended && !closed {
		csrf = csrfToken(identity)
	}
	submitKey := "meet.submitAnswer"
	if formID != "" {
		submitKey = "meet.saveChanges"
	}
	var modeKey string
	switch v.VoteType {
	case database.VoteTypeYes:
		modeKey = "vote.modeYes"
	case database.VoteTypeYesNo:
		modeKey = "vote.modeYesNo"
	case database.VoteTypeFreeText:
		modeKey = "vote.modeFreeText"
	default:
		modeKey = "vote.modeYesMaybeNo"
	}
	ends := v.Expires.UTC().Format("2006-01-02")
	endsKey := slotKey(v.Expires)
	// The auto-deletion date (finished + retention) always shows on the
	// meta line; the closure moment, a datetime, only while the vote is
	// closed and stamped (legacy closed rows carry no stamp). The vote
	// has no zone of its own, so both render in UTC like the end date.
	deletes := v.DeletionDate(cfg.FinishedTTL)
	closedAt := ""
	var closedAtKey string
	if v.Closed && v.ClosedAt != nil {
		closedAt = v.ClosedAt.UTC().Format("2 Jan 2006 15:04")
		closedAtKey = slotKey(*v.ClosedAt)
	}
	// The final marks' option IDs: the columns both links highlight (the
	// admin view adds the header's toggle buttons).
	finalIDs := make([]string, 0, len(v.FinalChoices))
	finalIDs = append(finalIDs, v.FinalChoices...)

	return renderPage(c, "vote", "pagetitle.meet", v.Title, &pageData{
		Vote: &votePage{
			answerPage: answerPage{
				c:           c,
				Title:       v.Title,
				Description: v.Description,
				Email:       v.Email,
				Ends:        ends,
				EndsKey:     endsKey,
				Deletes:     deletes.UTC().Format("2006-01-02"),
				DeletesKey:  slotKey(deletes),
				ClosedAt:    closedAt,
				ClosedAtKey: closedAtKey,
				Ended:       ended,
				Closed:      closed,
				IsAdmin:     isAdmin,
				UserLink:    absURL(c, "/vote/"+v.Key),
				AdminLink:   absURL(c, "/vote/"+v.AdminKey),
				EditLink:    "/vote-form/edit/" + v.AdminKey,
				// The lifecycle forms post through the admin link (a
				// capability URL) — they only render in the admin view.
				CloseAction:      "/vote/" + v.AdminKey + "/close",
				ReopenAction:     "/vote/" + v.AdminKey + "/reopen",
				DeletePollAction: "/vote/" + v.AdminKey + "/delete",
				FinalAction:      "/vote/" + v.AdminKey + "/final",
				Final:            finalIDs,
				HasFinal:         len(v.FinalChoices) > 0,
				ReopenWarnKey:    "vote.reopenWarn",
				SelfLink:         "/vote/" + viewKey,
				FormNew:          formNew,
				FormID:           formID,
				FormName:         formName,
				FormAction:       formAction,
				FormRespondentID: formRespondentID,
				DeleteAction:     "/vote/" + viewKey + "/answer/delete",
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
			Single:      v.SingleChoice,
			MustAnswer:  v.MustAnswer,
			Choices:     v.Choices,
			ModeKey:     modeKey,
			VoteType:    v.VoteType,
			VoteCols:    cols,
			VoteFormCol: formCol,
			Totals:      totals,
			HasFooter:   (isAdmin && closed) || len(v.FinalChoices) > 0,
		},
	})
}
