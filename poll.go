package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// ---- poll identity cookie ----

// cookieName builds the per-poll identity cookie name from the user link
// key. Both links share one identity this way, and the poll's cookie can
// be dropped (answer deleted) without touching other polls' cookies.
func cookieName(m *database.Meeting) string {
	return "kiusan-" + m.Key
}

// legacyCookieAge is how long an identity cookie lives when the poll has
// no end date (created before the expiry feature).
const legacyCookieAge = 90 * 24 * time.Hour

// pollEnded reports whether the poll has reached its end (nil = no end).
func pollEnded(m *database.Meeting, now time.Time) bool {
	return m.Expires != nil && now.After(*m.Expires)
}

// ensureIdentityCookie returns the poll's identity value for this
// request: the one the browser sent, or a freshly minted one (set on the
// response) when the browser has none yet and the poll is still running.
// Returning the value — not just setting the cookie — lets the very first
// page load already carry a CSRF token matching the cookie it mints.
func ensureIdentityCookie(c fiber.Ctx, m *database.Meeting, now time.Time) string {
	name := cookieName(m)
	if id := c.Cookies(name); id != "" {
		return id
	}
	exp := *m.Expires
	if m.Expires == nil {
		exp = now.Add(legacyCookieAge)
	}
	if !exp.After(now) {
		return ""
	}
	token, err := store.RandomKey(32)
	if err != nil {
		return ""
	}
	c.Cookie(&fiber.Cookie{
		Name: name, Value: token, Expires: exp,
		HTTPOnly: true, SameSite: "Lax", Path: "/",
	})
	return token
}

// dropIdentityCookie expires the poll's identity cookie, called when the
// respondent deletes their answer.
func dropIdentityCookie(c fiber.Ctx, m *database.Meeting) {
	c.Cookie(&fiber.Cookie{
		Name: cookieName(m), Value: "", Expires: time.Unix(0, 0), MaxAge: 0,
		HTTPOnly: true, SameSite: "Lax", Path: "/",
	})
}

// ---- CSRF protection ----

// csrfSecret signs the per-identity CSRF tokens. It is random per process
// unless KIUSAN_SECRET is set, which keeps still-open pages able to submit
// across a server restart.
var csrfSecret = func() []byte {
	if s := os.Getenv("KIUSAN_SECRET"); s != "" {
		return []byte(s)
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		log.Printf("warning: could not generate CSRF secret: %v", err)
	}
	return b
}()

// csrfToken is the per-identity form token: an HMAC over the identity
// cookie value. A forged cross-site POST carries no cookie (SameSite) and
// cannot mint a token (the cookie value is HttpOnly), so token plus
// cookie pin the request to the page that rendered it.
func csrfToken(identity string) string {
	mac := hmac.New(sha256.New, csrfSecret)
	mac.Write([]byte(identity))
	return hex.EncodeToString(mac.Sum(nil))
}

// verifyCSRF checks the csrftoken form field against the identity cookie.
func verifyCSRF(c fiber.Ctx, identity string) bool {
	if identity == "" {
		return false
	}
	return hmac.Equal([]byte(c.FormValue("csrftoken")), []byte(csrfToken(identity)))
}

// isFormPost reports whether the request body is a classic urlencoded
// form — the only shape this site's forms produce; anything else is
// rejected on the state-changing routes.
func isFormPost(c fiber.Ctx) bool {
	return strings.HasPrefix(strings.ToLower(c.Get("Content-Type")), "application/x-www-form-urlencoded")
}

// ---- slots & answer lists ----

// slotKey identifies a slot by its absolute instant, so edits to the poll
// (add/remove/reorder slots) never shift the instant stored with an
// answer; answers mark the moment, not a row number.
func slotKey(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04Z07:00")
}

// findAnswer returns the answer stored under the identity token, if any.
func findAnswer(list database.AnswerList, id string) *database.Answer {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}

// upsertAnswer replaces or appends the answer stored under the identity
// token, so editing an answer keeps the entry.
func upsertAnswer(list database.AnswerList, id, name string, marks map[string]string, now time.Time) database.AnswerList {
	for i := range list {
		if list[i].ID == id {
			list[i].Name = name
			list[i].Marks = marks
			list[i].UpdatedAt = now
			return list
		}
	}
	return append(list, database.Answer{ID: id, Name: name, Marks: marks, UpdatedAt: now})
}

// removeAnswer drops the answer stored under the identity token.
func removeAnswer(list database.AnswerList, id string) (database.AnswerList, bool) {
	for i := range list {
		if list[i].ID == id {
			return append(list[:i], list[i+1:]...), true
		}
	}
	return list, false
}

// pruneAnswers drops stored marks of slots the meeting no longer offers,
// called when the poll is edited.
func pruneAnswers(m *database.Meeting) {
	instant := make(map[string]struct{}, len(m.Dates))
	for _, d := range m.Dates {
		instant[slotKey(d)] = struct{}{}
	}
	for i := range m.Answers {
		for k := range m.Answers[i].Marks {
			if _, ok := instant[k]; !ok {
				delete(m.Answers[i].Marks, k)
			}
		}
	}
}

// validName trims and bounds the respondent name.
func validName(raw string) (string, string) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", "Name is required"
	}
	if len([]rune(name)) > 50 {
		return "", "Name is too long (max 50 characters)"
	}
	return name, ""
}

// collectMarks reads the per-slot answer fields (name="a<slotkey>") of a
// submission and returns the marked slots plus a 400 message on violation.
// In single-slot polls only "g" is accepted and at most one slot may be
// marked — the client-side greying is UI, this is the tamper guard.
func collectMarks(c fiber.Ctx, m *database.Meeting) (map[string]string, string) {
	marks := make(map[string]string, len(m.Dates))
	greens := 0
	for _, slot := range m.Dates {
		v := c.FormValue("a" + slotKey(slot))
		switch v {
		case "", "g", "y":
		default:
			return nil, "Invalid answer option"
		}
		if m.SingleSignup && (v == "y" || (v == "g" && greens > 0)) {
			return nil, "This poll allows signing up for only one time slot"
		}
		if v == "g" {
			greens++
		}
		if v != "" {
			marks[slotKey(slot)] = v
		}
	}
	return marks, ""
}

// ---- meeting load/save with compare-and-set ----

var (
	// errNotFoundPoll: the link key resolves to no meeting.
	errNotFoundPoll = errors.New("poll: link unknown")
	// errConflict: all compare-and-set retries were exhausted.
	errConflict = errors.New("poll: update conflict")
)

// loadMeeting fetches the meeting by user key (byAdmin=false) or admin key.
func loadMeeting(key string, byAdmin bool) (*database.Meeting, error) {
	if byAdmin {
		return db.GetAdmin(key)
	}
	return db.Get(key)
}

// saveMeeting re-reads the meeting, applies mutate to it, and writes it
// back under the compare-and-set, retrying on conflict so two concurrent
// writers both land. It returns the stored copy, or errNotFoundPoll /
// errConflict / a storage error.
func saveMeeting(key string, byAdmin bool, mutate func(m *database.Meeting)) (*database.Meeting, error) {
	for attempt := 0; attempt < 3; attempt++ {
		m, err := loadMeeting(key, byAdmin)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, errNotFoundPoll
			}
			return nil, err
		}
		mutate(m)
		if err := db.Update(m); err != nil {
			if errors.Is(err, store.ErrConflict) {
				continue
			}
			return nil, err
		}
		return m, nil
	}
	return nil, errConflict
}

// resolveMeeting finds the meeting behind a link key and reports which
// view the key matched: the user key renders the respondent page, the
// admin key the admin page. A missing key yields a nil meeting, not an
// error.
func resolveMeeting(key string) (*database.Meeting, bool, error) {
	m, err := db.Get(key)
	if err == nil {
		return m, false, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return nil, false, err
	}
	am, err := db.GetAdmin(key)
	if err == nil {
		return am, true, nil
	}
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	return nil, false, err
}
