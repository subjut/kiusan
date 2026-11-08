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

	return renderPage(c, "vote", "pagetitle.meet", v.Title, &pageData{
		Vote: &votePage{
			answerPage: answerPage{
				c:           c,
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
				EditLink:    "/vote-form/edit/" + v.AdminKey,
				// The lifecycle forms post through the admin link (a
				// capability URL) — they only render in the admin view.
				CloseAction:      "/vote/" + v.AdminKey + "/close",
				ReopenAction:     "/vote/" + v.AdminKey + "/reopen",
				DeletePollAction: "/vote/" + v.AdminKey + "/delete",
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
		},
	})
}
