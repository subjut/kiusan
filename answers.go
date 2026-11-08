package main

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
)

// ---- shared answer & lifecycle handlers (meeting + vote) ----

// answerData is the type-erased payload of one submitted answer: the
// per-slot/per-choice marks, plus the per-choice notes a free-text vote
// collects (absent on meeting answers).
type answerData struct {
	marks map[string]string
	notes map[string]string
}

// pollOps is the type-specific surface of one answers table (the
// meeting's or the vote's); the generic handlers below drive both
// through it, so the two link shapes never diverge in their gates,
// redirects, or cookie handling.
type pollOps[T any] struct {
	// base is the redirect root ("/meet" | "/vote"); endedMsg the 410
	// body.
	base     string
	endedMsg string

	// resolve finds the entity behind a link key (user key first, then
	// admin key) and reports the view; a missing key yields a nil entity,
	// not an error (exists recognizes the nil).
	resolve func(key string) (T, bool, error)
	// exists recognizes resolve's nil result; keyOf is the entity's user
	// key (the CAS save key); cookie its identity cookie name;
	// ended/closed/setClosed the end check, the close flag, and the
	// lifecycle mutation; hasAns the answer's existence check.
	exists    func(e T) bool
	keyOf     func(e T) string
	cookie    func(e T) string
	ended     func(e T) bool
	closed    func(e T) bool
	setClosed func(e T, closed bool)
	hasAns    func(e T, id string) bool

	// collect reads the answer fields of a submission (400 message on
	// violation); upsert/remove apply an answer to the entity's list.
	collect func(c fiber.Ctx, e T) (answerData, string)
	upsert  func(e T, id, name string, d answerData, now time.Time)
	remove  func(e T, id string)

	// save is the compare-and-set round trip; purge deletes the row.
	save  func(key string, byAdmin bool, mutate func(T)) (T, error)
	purge func(e T) error
}

// The route handlers: each returns the generic handler bound to this
// answers table, so the routes read `meetOps.postAnswer()`.
func (o *pollOps[T]) postAnswer() fiber.Handler {
	return func(c fiber.Ctx) error { return answerPost(o, c) }
}

func (o *pollOps[T]) editAnswer() fiber.Handler {
	return func(c fiber.Ctx) error { return answerEditPost(o, c) }
}

func (o *pollOps[T]) deleteAnswer() fiber.Handler {
	return func(c fiber.Ctx) error { return answerDeletePost(o, c) }
}

func (o *pollOps[T]) closePoll() fiber.Handler {
	return func(c fiber.Ctx) error { return closePollPost(o, c) }
}

func (o *pollOps[T]) reopenPoll() fiber.Handler {
	return func(c fiber.Ctx) error { return reopenPollPost(o, c) }
}

func (o *pollOps[T]) deletePoll() fiber.Handler {
	return func(c fiber.Ctx) error { return deletePollPost(o, c) }
}

// answerPost stores or updates the answer of the respondent behind the
// request — the identity cookie; either link may submit it.
func answerPost[T any](o *pollOps[T], c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	e, _, err := o.resolve(c.Params("+"))
	if err != nil {
		return sendInternal(c, err)
	}
	if !o.exists(e) {
		return notFoundHandler(c)
	}
	if o.closed(e) || o.ended(e) {
		return c.Status(410).SendString(o.endedMsg)
	}
	identity := c.Cookies(o.cookie(e))
	if identity == "" || !verifyCSRF(c, identity) {
		return c.Status(401).SendString("Unknown or stale identity — reload the page and try again")
	}
	name, msg := validName(c.FormValue("name"))
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	data, msg := o.collect(c, e)
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	if _, err := o.save(o.keyOf(e), false, func(e T) {
		o.upsert(e, identity, name, data, time.Now())
	}); err != nil {
		return saveError(c, err)
	}
	return c.Redirect().To(o.base + "/" + c.Params("+"))
}

// answerEditPost lets the admin (admin link only) edit any respondent's
// answer.
func answerEditPost[T any](o *pollOps[T], c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	e, isAdmin, err := o.resolve(key)
	if err != nil {
		return sendInternal(c, err)
	}
	// The route exists for both link shapes; only the admin link may use
	// it, so a user key looks up like a missing link.
	if !o.exists(e) || !isAdmin {
		return notFoundHandler(c)
	}
	if o.closed(e) || o.ended(e) {
		return c.Status(410).SendString(o.endedMsg)
	}
	id := strings.TrimSpace(c.FormValue("respondentid"))
	if !o.hasAns(e, id) {
		return notFoundHandler(c)
	}
	name, msg := validName(c.FormValue("name"))
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	data, msg := o.collect(c, e)
	if msg != "" {
		return c.Status(400).SendString(msg)
	}
	if _, err := o.save(key, true, func(e T) {
		o.upsert(e, id, name, data, time.Now())
	}); err != nil {
		return saveError(c, err)
	}
	return c.Redirect().To(o.base + "/" + key)
}

// answerDeletePost deletes an answer: via the admin link any
// respondent's; via the user link only the own (identity cookie) answer,
// which also expires the identity cookie.
func answerDeletePost[T any](o *pollOps[T], c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	e, isAdmin, err := o.resolve(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if !o.exists(e) {
		return notFoundHandler(c)
	}
	if o.closed(e) || o.ended(e) {
		return c.Status(410).SendString(o.endedMsg)
	}
	id := strings.TrimSpace(c.FormValue("respondentid"))
	if id == "" {
		return c.Status(400).SendString("Missing respondent")
	}
	identity := c.Cookies(o.cookie(e))
	if !isAdmin && id != identity {
		// A user link may only ever act on the own identity.
		return notFoundHandler(c)
	}
	if !o.hasAns(e, id) {
		return notFoundHandler(c)
	}
	if !isAdmin && !verifyCSRF(c, identity) {
		return c.Status(401).SendString("Unknown or stale identity — reload the page and try again")
	}
	if _, err := o.save(key, isAdmin, func(e T) {
		o.remove(e, id)
	}); err != nil {
		return saveError(c, err)
	}
	if !isAdmin {
		dropIdentityCookie(c, o.cookie(e))
	}
	return c.Redirect().To(o.base + "/" + key)
}

// ---- shared lifecycle (admin link only) ----

// closePollPost closes a running poll or vote: it stops accepting answers
// until the admin re-opens it, while both links keep rendering the answers
// table.
func closePollPost[T any](o *pollOps[T], c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	e, isAdmin, err := o.resolve(key)
	if err != nil {
		return sendInternal(c, err)
	}
	// The route exists for both link shapes; only the admin link may use
	// it, so a user key looks up like a missing link.
	if !o.exists(e) || !isAdmin {
		return notFoundHandler(c)
	}
	if o.ended(e) {
		return c.Status(410).SendString(o.endedMsg)
	}
	if _, err := o.save(key, true, func(e T) {
		o.setClosed(e, true)
	}); err != nil {
		return saveError(c, err)
	}
	return c.Redirect().To(o.base + "/" + key)
}

// reopenPollPost re-opens a closed poll or vote: new answers can be
// submitted and existing ones edited or deleted again.
func reopenPollPost[T any](o *pollOps[T], c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	e, isAdmin, err := o.resolve(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if !o.exists(e) || !isAdmin {
		return notFoundHandler(c)
	}
	if _, err := o.save(key, true, func(e T) {
		o.setClosed(e, false)
	}); err != nil {
		return saveError(c, err)
	}
	return c.Redirect().To(o.base + "/" + key)
}

// deletePollPost purges the poll or vote (admin link only): its row with
// every answer, and with it each respondent's stored identity. The
// browser-side identity cookie is dropped in the redirect, where it goes
// inert.
func deletePollPost[T any](o *pollOps[T], c fiber.Ctx) error {
	if !isFormPost(c) {
		return c.Status(415).SendString("Unsupported content type")
	}
	key := c.Params("+")
	e, isAdmin, err := o.resolve(key)
	if err != nil {
		return sendInternal(c, err)
	}
	if !o.exists(e) || !isAdmin {
		return notFoundHandler(c)
	}
	if err := o.purge(e); err != nil {
		return sendInternal(c, err)
	}
	dropIdentityCookie(c, o.cookie(e))
	return c.Redirect().To("/")
}

// meetOps drives the meeting's answers table.
var meetOps = &pollOps[*database.Meeting]{
	base:     "/meet",
	endedMsg: "This poll has ended",
	resolve:  resolveMeeting,
	exists:   func(m *database.Meeting) bool { return m != nil },
	keyOf:    func(m *database.Meeting) string { return m.Key },
	cookie:   cookieName,
	ended:    func(m *database.Meeting) bool { return !m.Expires.After(time.Now()) },
	closed:   func(m *database.Meeting) bool { return m.Closed },
	setClosed: func(m *database.Meeting, closed bool) {
		m.Closed = closed
	},
	hasAns: func(m *database.Meeting, id string) bool {
		return findAnswer(m.Answers, id) != nil
	},
	collect: func(c fiber.Ctx, m *database.Meeting) (answerData, string) {
		marks, msg := collectMarks(c, m)
		if msg != "" {
			return answerData{}, msg
		}
		return answerData{marks: marks}, ""
	},
	upsert: func(m *database.Meeting, id, name string, d answerData, now time.Time) {
		m.Answers = upsertAnswer(m.Answers, id, name, d.marks, now)
	},
	remove: func(m *database.Meeting, id string) {
		m.Answers, _ = removeAnswer(m.Answers, id)
	},
	save:  saveMeeting,
	purge: func(m *database.Meeting) error { return db.Delete(m.Key) },
}

// voteOps drives the vote's answers table.
var voteOps = &pollOps[*database.Vote]{
	base:     "/vote",
	endedMsg: "This vote has ended",
	resolve:  resolveVote,
	exists:   func(v *database.Vote) bool { return v != nil },
	keyOf:    func(v *database.Vote) string { return v.Key },
	cookie:   voteCookieName,
	ended:    func(v *database.Vote) bool { return !v.Expires.After(time.Now()) },
	closed:   func(v *database.Vote) bool { return v.Closed },
	setClosed: func(v *database.Vote, closed bool) {
		v.Closed = closed
	},
	hasAns: func(v *database.Vote, id string) bool {
		return findVoteAnswer(v.Answers, id) != nil
	},
	collect: func(c fiber.Ctx, v *database.Vote) (answerData, string) {
		marks, notes, msg := collectVoteAnswers(c, v)
		if msg != "" {
			return answerData{}, msg
		}
		return answerData{marks: marks, notes: notes}, ""
	},
	upsert: func(v *database.Vote, id, name string, d answerData, now time.Time) {
		v.Answers = upsertVoteAnswer(v.Answers, id, name, d.marks, d.notes, now)
	},
	remove: func(v *database.Vote, id string) {
		v.Answers, _ = removeVoteAnswer(v.Answers, id)
	},
	save:  saveVote,
	purge: func(v *database.Vote) error { return db.DeleteVote(v.Key) },
}
