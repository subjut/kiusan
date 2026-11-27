package main

import (
	"errors"
	"testing"
	"time"

	"github.com/subjut/kiusan/database"
	"github.com/subjut/kiusan/store"
)

// TestPurgeFinished pins one maintenance pass end to end: finished polls
// and votes whose retention (finished + months, the earlier of closure
// and end date) has passed are gone; everything younger survives, and a
// passing purge compacts the backend without losing data.
func TestPurgeFinished(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	newTestApp(t)

	now := time.Now()
	stamp := func(dt time.Time) *time.Time { return &dt }
	poll := func(title string, closed bool, closedAt *time.Time, expires time.Time) string {
		t.Helper()
		m := &database.Meeting{
			Title: title, Description: "d", Email: "e@example.com",
			Dates:    database.TimeSlice{now.Add(-24 * time.Hour)},
			TimeZone: "UTC", Expires: expires,
		}
		key, err := db.Create(m)
		if err != nil {
			t.Fatalf("creating %q: %v", title, err)
		}
		if closed || closedAt != nil {
			got, err := db.Get(key)
			if err != nil {
				t.Fatalf("reading %q: %v", title, err)
			}
			got.Closed = closed
			got.ClosedAt = closedAt
			if err := db.Update(got); err != nil {
				t.Fatalf("updating %q: %v", title, err)
			}
		}
		return key
	}
	vote := func(title string, expires time.Time) string {
		t.Helper()
		v := &database.Vote{
			Title: title, Description: "d", Email: "e@example.com",
			VoteType: database.VoteTypeYesNo,
			Choices:  database.ChoiceList{{ID: "c1", Text: "A"}},
			Expires:  expires,
		}
		key, err := db.CreateVote(v)
		if err != nil {
			t.Fatalf("creating vote %q: %v", title, err)
		}
		return key
	}

	open := poll("open", false, nil, now.AddDate(0, 1, 0))
	closedFresh := poll("closed-fresh", true, stamp(now.AddDate(0, -1, 0)), now.AddDate(0, 1, 0))
	closedOld := poll("closed-old", true, stamp(now.AddDate(0, -7, 0)), now.AddDate(0, 1, 0))
	legacyClosed := poll("legacy-closed", true, nil, now.AddDate(0, -7, 0))
	expiredOld := poll("expired-old", false, nil, now.AddDate(0, -7, 0))
	expiredFresh := poll("expired-fresh", false, nil, now.AddDate(0, -1, 0))
	voteOpen := vote("vote-open", now.AddDate(0, 1, 0))
	voteOld := vote("vote-old", now.AddDate(0, -7, 0))

	purged, purgedVotes := purgeFinished(db, now, 6)
	if purged != 3 || purgedVotes != 1 {
		t.Fatalf("purge counts: %d polls, %d votes — want 3 polls (closed-old, legacy-closed, expired-old), 1 vote", purged, purgedVotes)
	}
	for _, key := range []string{closedOld, legacyClosed, expiredOld, voteOld} {
		_, err := db.Get(key)
		if !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("purged poll %q: err %v, want ErrNotFound", key, err)
		}
	}
	if _, err := db.GetVote(voteOld); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("purged vote: err %v, want ErrNotFound", err)
	}
	for _, key := range []string{open, closedFresh, expiredFresh} {
		if _, err := db.Get(key); err != nil {
			t.Fatalf("surviving poll %q: %v", key, err)
		}
	}
	if _, err := db.GetVote(voteOpen); err != nil {
		t.Fatalf("surviving vote: %v", err)
	}
	// The compaction ran with data present: the store still serves it.
	if _, err := db.Get(open); err != nil {
		t.Fatalf("post-compaction read: %v", err)
	}
}

// TestPurgeFinishedImageSweep pins the janitor's unreferenced-image sweep:
// an image a stored vote names survives, an abandoned upload (past the 7
// day window) is purged, and a fresh upload (a half-filled form's, whose
// window has not closed) survives.
func TestPurgeFinishedImageSweep(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	newTestApp(t)

	now := time.Now()
	referenced, abandoned, fresh := "img-ref1234567", "img-old1234567", "img-new1234567"
	if err := db.PutVoteImage(referenced, []byte("t"), []byte("f"), now); err != nil {
		t.Fatalf("storing referenced image: %v", err)
	}
	if err := db.PutVoteImage(abandoned, []byte("t"), []byte("f"), now.Add(-8*24*time.Hour)); err != nil {
		t.Fatalf("storing abandoned image: %v", err)
	}
	if err := db.PutVoteImage(fresh, []byte("t"), []byte("f"), now.Add(-time.Hour)); err != nil {
		t.Fatalf("storing fresh image: %v", err)
	}

	// The referenced image is named by a live vote's choice.
	v := &database.Vote{
		Title: "t", Description: "d", Email: "e@e.com",
		VoteType: database.VoteTypeYesNo,
		Choices:  database.ChoiceList{{ID: "c1", Text: "A", Image: referenced}},
		Expires:  now.AddDate(0, 1, 0),
	}
	if _, err := db.CreateVote(v); err != nil {
		t.Fatalf("creating vote: %v", err)
	}

	purgeFinished(db, now, 6)

	if _, err := db.GetVoteImage(abandoned, database.VoteImageKindThumb); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("the abandoned upload must be purged: %v", err)
	}
	for _, id := range []string{referenced, fresh} {
		if _, err := db.GetVoteImage(id, database.VoteImageKindThumb); err != nil {
			t.Fatalf("surviving image %q: %v", id, err)
		}
	}
}

// TestRunJanitor starts the maintenance loop (one pass at start, then the
// 24 h ticker) against a throwaway store — the pass is a no-op on empty
// data, and the loop's goroutine is harmless past the test's end.
func TestRunJanitor(t *testing.T) {
	withStore(t, "sqlite", ":memory:")
	newTestApp(t)
	runJanitor(db, 6)
}
