package main

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// ---- vote form: create & edit ----

// voteForm is the vote create/edit form's page data. On edit the stored
// choices carry their stable IDs, which the form renders as hidden
// fields so unchanged options keep their ID (and their answers).
type voteForm struct {
	formBase
	VoteKey      string
	Choices      database.ChoiceList
	SingleChoice bool
	MustAnswer   bool
	VoteType     string
}

func voteFormHandler(c fiber.Ctx) error {
	now := time.Now()
	return renderPage(c, "vote-form", "pagetitle.voteForm", "", &pageData{
		VoteForm: &voteForm{
			formBase: formBase{
				FormTitleKey: "form.voteCreateTitle",
				DefaultExp:   now.UTC().AddDate(0, 3, 0).Format("2006-01-02"),
				MinExp:       now.UTC().Format("2006-01-02"),
				SubmitLabel:  translator.MustLocalize(c, "form.submit"),
			},
			// A new vote defaults to the yes-no type; the template checks
			// the radios off VoteType, so an empty type checks yes/no there.
			VoteType: database.VoteTypeYesNo,
		},
	})
}

func voteFormEditHandler(c fiber.Ctx) error {
	v, err := db.GetVoteAdmin(c.Params("+"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return linkGone(c)
		}
		return sendInternal(c, err)
	}

	return renderPage(c, "vote-form", "pagetitle.voteFormEdit", v.Title, &pageData{
		VoteForm: &voteForm{
			formBase: formBase{
				Title:        v.Title,
				Email:        v.Email,
				Description:  v.Description,
				HideUsers:    v.HideUsers,
				FormTitleKey: "form.voteEditTitle",
				Expiry:       v.Expires.UTC().Format("2006-01-02"),
				MinExp:       time.Now().UTC().Format("2006-01-02"),
				SubmitLabel:  translator.MustLocalize(c, "form.saveChanges"),
			},
			VoteKey:      v.AdminKey,
			Choices:      v.Choices,
			SingleChoice: v.SingleChoice,
			MustAnswer:   v.MustAnswer,
			VoteType:     v.VoteType,
		},
	})
}

func voteFormPostHandler(c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	var v database.Vote

	if err := c.Bind().Body(&v); err != nil {
		return sendErr(c, 400, "Invalid form data")
	}
	if msg := validateTextFields(v.Title, v.Email, v.Description); msg != "" {
		return sendErr(c, 400, msg)
	}

	v.HideUsers = c.FormValue("hide-users") != ""
	v.SingleChoice = c.FormValue("single-choice") != ""
	v.MustAnswer = c.FormValue("must-answer") != ""
	v.VoteType = strings.TrimSpace(c.FormValue("votetype"))
	switch v.VoteType {
	case database.VoteTypeYes, database.VoteTypeYesNo, database.VoteTypeYesMaybeNo, database.VoteTypeFreeText:
	case "":
		return sendErr(c, 400, "No vote type selected")
	default:
		return sendErr(c, 400, "Invalid vote type")
	}
	// The form greys out the disabled boxes/radios: single-choice only
	// works with the yes and freetext types, must-answer only with
	// yes-no, yes-maybe-no, and freetext, and the two checkboxes are
	// mutually exclusive. These are the server-side guards against a
	// hand-built submission.
	if v.SingleChoice && v.MustAnswer {
		return sendErr(c, 400, "Invalid answer mode")
	}
	if v.SingleChoice && v.VoteType != database.VoteTypeYes && v.VoteType != database.VoteTypeFreeText {
		return sendErr(c, 400, "Invalid vote type")
	}
	if v.MustAnswer && v.VoteType == database.VoteTypeYes {
		return sendErr(c, 400, "Invalid vote type")
	}

	// The option rows arrive as (choice0, choiceID0), (choice1,
	// choiceID1), ... — on edit the ID is the one the form rendered, so a
	// row whose text is unchanged keeps its ID (and its answers).
	rows := make([]choiceRow, 0, 4)
	for i := 0; ; i++ {
		text := strings.TrimSpace(c.FormValue("choice" + strconv.Itoa(i)))
		if text == "" {
			break
		}
		if len(rows) >= maxChoices {
			return sendErr(c, 400, "Too many choices (max 20)")
		}
		if utf8.RuneCountInString(text) > maxChoiceLen {
			return sendErr(c, 400, "Choice too long (max 100 characters)")
		}
		rows = append(rows, choiceRow{
			text:  text,
			oldID: strings.TrimSpace(c.FormValue("choiceID" + strconv.Itoa(i))),
		})
	}
	if len(rows) == 0 {
		return sendErr(c, 400, "At least one choice is required")
	}

	endDate, msg := parseEndDate(c.FormValue("enddate"), time.Now())
	if msg != "" {
		return sendErr(c, 400, msg)
	}
	v.Expires = endOf(endDate, time.UTC)

	if editKey := strings.TrimSpace(c.FormValue("votekey")); editKey != "" {
		old, err := db.GetVoteAdmin(editKey)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return notFoundHandler(c)
			}
			return sendInternal(c, err)
		}
		choices, err := assignChoiceIDs(old.Choices, rows)
		if err != nil {
			return sendInternal(c, err)
		}
		v.Choices = choices
		saved, err := saveVote(editKey, true, func(vote *database.Vote) {
			vote.Title = v.Title
			vote.Description = v.Description
			vote.Email = v.Email
			vote.HideUsers = v.HideUsers
			vote.SingleChoice = v.SingleChoice
			vote.MustAnswer = v.MustAnswer
			vote.VoteType = v.VoteType
			vote.Choices = v.Choices
			vote.Expires = v.Expires
			pruneVoteAnswers(vote)
		})
		if err != nil {
			return saveError(c, err)
		}
		return c.Redirect().To("/vote/" + saved.AdminKey)
	}

	choices, err := assignChoiceIDs(nil, rows)
	if err != nil {
		return sendInternal(c, err)
	}
	v.Choices = choices
	if _, err := db.CreateVote(&v); err != nil {
		return sendInternal(c, err)
	}
	return c.Redirect().To("/vote/" + v.AdminKey)
}
