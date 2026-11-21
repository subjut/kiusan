package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/subjut/kiusan/database"
)

// openTestStore opens a throwaway in-memory sqlite store that closes when
// the test finishes. The sqlite engine covers every gormStore method —
// the relational backends share the same code path behind the dialector.
func openTestStore(t *testing.T) Store {
	t.Helper()
	s, err := Open(BackendSQLite, ":memory:")
	if err != nil {
		t.Fatalf("opening sqlite :memory: store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// sampleMeeting populates every stored field, so a create -> get round
// trip can assert field integrity.
func sampleMeeting() *database.Meeting {
	return &database.Meeting{
		Title:       "Sample",
		Description: "A sample poll",
		Email:       "s@example.com",
		HideUsers:   true,
		Dates:       database.TimeSlice{time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC)},
		TimeZone:    "UTC",
		Expires:     time.Date(2026, 8, 4, 23, 59, 59, 0, time.UTC),
		Answers:     database.AnswerList{{ID: "id-1", Name: "Alice", Marks: map[string]string{"2026-05-04T09:30Z07:00": "g"}}},
	}
}

// sampleVote populates every stored field of a vote.
func sampleVote() *database.Vote {
	return &database.Vote{
		Title:       "Sample vote",
		Description: "Pick one",
		Email:       "v@example.com",
		HideTotals:  true,
		VoteType:    database.VoteTypeYesNo,
		Choices:     database.ChoiceList{{ID: "c1", Text: "A"}, {ID: "c2", Text: "B"}},
		Expires:     time.Date(2027, 2, 1, 23, 59, 59, 0, time.UTC),
		Answers:     database.VoteAnswerList{{ID: "u1", Name: "Ada", Marks: map[string]string{"c1": "y"}}},
	}
}

// TestOpenDispatch pins the backend selection: the empty kind is sqlite,
// sqlite falls back to the default data file, the relational backends
// need a DSN, and an unknown kind is rejected.
func TestOpenDispatch(t *testing.T) {
	t.Run("empty kind defaults to sqlite with the default data file", func(t *testing.T) {
		t.Chdir(t.TempDir())
		s, err := Open("", "")
		if err != nil {
			t.Fatalf("Open(\"\", \"\"): %v", err)
		}
		if err := s.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		if _, err := os.Stat(defaultSqliteName); err != nil {
			t.Fatalf("default data file %q not created: %v", defaultSqliteName, err)
		}
	})
	if _, err := Open(BackendSQLite, ":memory:"); err != nil {
		t.Fatalf("Open(sqlite, :memory:): %v", err)
	}
	for _, kind := range []string{BackendMySQL, BackendMariaDB, BackendPostgres} {
		if _, err := Open(kind, ""); err == nil {
			t.Fatalf("Open(%s, \"\"): expected an error for a missing DSN", kind)
		}
	}
	if _, err := Open("oracle", ":memory:"); err == nil {
		t.Fatalf("Open(oracle): expected an error for an unknown backend")
	}
}

// TestMeetingLifecycle covers create (key minting), the two-link
// resolution, the field round trip, and delete.
func TestMeetingLifecycle(t *testing.T) {
	s := openTestStore(t)
	m := sampleMeeting()
	key, err := s.Create(m)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if key != m.Key || len(key) != 16 {
		t.Fatalf("Create: user key %q not minted (returned %q)", m.Key, key)
	}
	if m.AdminKey == "" || len(m.AdminKey) != 16 || m.AdminKey == key {
		t.Fatalf("Create: admin key %q must be minted and distinct from the user key", m.AdminKey)
	}

	got, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "Sample" || got.Description != "A sample poll" || got.Email != "s@example.com" ||
		!got.HideUsers || got.TimeZone != "UTC" || !got.Expires.Equal(m.Expires) ||
		len(got.Dates) != 1 || !got.Dates[0].Equal(m.Dates[0]) ||
		len(got.Answers) != 1 || got.Answers[0].Name != "Alice" ||
		got.Answers[0].Marks["2026-05-04T09:30Z07:00"] != "g" {
		t.Fatalf("Get: round trip lost fields: %+v", got)
	}

	// The two links resolve through their own column only.
	if _, err := s.Get(m.AdminKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(admin key): expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetAdmin(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAdmin(user key): expected ErrNotFound, got %v", err)
	}
	ad, err := s.GetAdmin(m.AdminKey)
	if err != nil {
		t.Fatalf("GetAdmin: %v", err)
	}
	if ad.Title != "Sample" {
		t.Fatalf("GetAdmin: wrong row: %+v", ad)
	}

	// Unknown keys are ErrNotFound.
	if _, err := s.Get("no-such-key-12345"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get(unknown): expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetAdmin("no-such-key-12345"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAdmin(unknown): expected ErrNotFound, got %v", err)
	}

	// Delete removes the row under both links.
	if err := s.Delete(key); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get after Delete: expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetAdmin(m.AdminKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAdmin after Delete: expected ErrNotFound, got %v", err)
	}
}

// TestUpdateCAS pins the compare-and-set: a fresh update lands and bumps
// the generation; a copy stamped with a stale generation conflicts and
// leaves the stored row untouched.
func TestUpdateCAS(t *testing.T) {
	s := openTestStore(t)
	m := sampleMeeting()
	key, err := s.Create(m)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	m.Title = "updated"
	m.Expires = time.Date(2027, 1, 1, 23, 59, 59, 0, time.UTC)
	if err := s.Update(m); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if m.Generation != 1 {
		t.Fatalf("Update: mirrored Generation %d, want 1", m.Generation)
	}
	got, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != "updated" || got.Generation != 1 || !got.Expires.Equal(m.Expires) {
		t.Fatalf("Update not stored: %+v", got)
	}

	// Rewind a copy's stamp: the stale write must conflict.
	stale, err := s.Get(key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	stale.Generation--
	stale.Title = "stale write"
	if err := s.Update(stale); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale Update: got %v, want ErrConflict", err)
	}
	got, _ = s.Get(key)
	if got.Title != "updated" || got.Generation != 1 {
		t.Fatalf("stale write must not land: %+v", got)
	}
}

// TestVoteLifecycle mirrors the meeting lifecycle for votes, plus the
// update carrying every mutable column (incl. hide-totals).
func TestVoteLifecycle(t *testing.T) {
	s := openTestStore(t)
	v := sampleVote()
	key, err := s.CreateVote(v)
	if err != nil {
		t.Fatalf("CreateVote: %v", err)
	}
	if key != v.Key || len(key) != 16 || v.AdminKey == "" || v.AdminKey == key {
		t.Fatalf("CreateVote: keys user %q admin %q", v.Key, v.AdminKey)
	}

	got, err := s.GetVote(key)
	if err != nil {
		t.Fatalf("GetVote: %v", err)
	}
	if got.Title != "Sample vote" || !got.HideTotals || got.VoteType != database.VoteTypeYesNo ||
		len(got.Choices) != 2 || got.Choices[0].ID != "c1" || !got.Expires.Equal(v.Expires) ||
		len(got.Answers) != 1 || got.Answers[0].Marks["c1"] != "y" {
		t.Fatalf("GetVote: round trip lost fields: %+v", got)
	}
	if _, err := s.GetVote(v.AdminKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetVote(admin key): expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetVoteAdmin(v.AdminKey); err != nil {
		t.Fatalf("GetVoteAdmin: %v", err)
	}
	if _, err := s.GetVote("no-such-key-12345"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetVote(unknown): expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetVoteAdmin("no-such-key-12345"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetVoteAdmin(unknown): expected ErrNotFound, got %v", err)
	}

	// Update carries every mutable column and bumps the generation.
	got.Title = "edited"
	got.HideTotals = false
	got.VoteType = database.VoteTypeYes
	if err := s.UpdateVote(got); err != nil {
		t.Fatalf("UpdateVote: %v", err)
	}
	if got.Generation != 1 {
		t.Fatalf("UpdateVote: mirrored Generation %d, want 1", got.Generation)
	}
	got2, err := s.GetVote(key)
	if err != nil {
		t.Fatalf("GetVote: %v", err)
	}
	if got2.Title != "edited" || got2.HideTotals || got2.VoteType != database.VoteTypeYes ||
		got2.Generation != 1 {
		t.Fatalf("UpdateVote not stored (all mutable columns, incl. hide_totals, must be carried): %+v", got2)
	}

	// The CAS conflict.
	got2.Generation--
	got2.Title = "stale"
	if err := s.UpdateVote(got2); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale UpdateVote: got %v, want ErrConflict", err)
	}

	if err := s.DeleteVote(key); err != nil {
		t.Fatalf("DeleteVote: %v", err)
	}
	if _, err := s.GetVote(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetVote after DeleteVote: expected ErrNotFound, got %v", err)
	}
	if _, err := s.GetVoteAdmin(v.AdminKey); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetVoteAdmin after DeleteVote: expected ErrNotFound, got %v", err)
	}
}

// TestAutoMigrateIdempotent opens a file-backed store, closes it, and
// reopens it: the second AutoMigrate over the existing schema must be a
// no-op and the store must keep working.
func TestAutoMigrateIdempotent(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "meetings.db")
	s1, err := Open(BackendSQLite, dsn)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := s1.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2, err := Open(BackendSQLite, dsn)
	if err != nil {
		t.Fatalf("re-Open over the existing schema: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })
	if _, err := s2.Create(sampleMeeting()); err != nil {
		t.Fatalf("Create after re-migration: %v", err)
	}
}

// TestPurgeFinished pins the store's retention sweep: finished (closed
// or expired) rows are removed only once they finished at least the
// given months ago (the earlier of closure and end date), everything
// younger survives — including a closed row whose closure is recent and
// a legacy closed row without a stamp (it falls back to the end date).
func TestPurgeFinished(t *testing.T) {
	s := openTestStore(t)
	now := time.Now()
	stamp := func(dt time.Time) *time.Time { return &dt }

	mk := func(title string, closed bool, closedAt *time.Time, expires time.Time) string {
		t.Helper()
		m := &database.Meeting{
			Title: title, Description: "d", Email: "e@example.com",
			Dates:    database.TimeSlice{time.Date(2026, 5, 4, 9, 30, 0, 0, time.UTC)},
			TimeZone: "UTC",
			Expires:  expires,
		}
		key, err := s.Create(m)
		if err != nil {
			t.Fatalf("Create %q: %v", title, err)
		}
		if closed || closedAt != nil {
			got, err := s.Get(key)
			if err != nil {
				t.Fatalf("Get %q: %v", title, err)
			}
			got.Closed = closed
			got.ClosedAt = closedAt
			if err := s.Update(got); err != nil {
				t.Fatalf("Update %q: %v", title, err)
			}
		}
		return key
	}

	open := mk("open", false, nil, now.AddDate(0, 1, 0))
	closedFresh := mk("closed-fresh", true, stamp(now.AddDate(0, -1, 0)), now.AddDate(0, 1, 0))
	closedOld := mk("closed-old", true, stamp(now.AddDate(0, -7, 0)), now.AddDate(0, 1, 0))
	legacy := mk("legacy-closed", true, nil, now.AddDate(0, -7, 0))
	expiredOld := mk("expired-old", false, nil, now.AddDate(0, -7, 0))
	expiredFresh := mk("expired-fresh", false, nil, now.AddDate(0, -1, 0))

	n, err := s.PurgeFinished(now, 6)
	if err != nil {
		t.Fatalf("PurgeFinished: %v", err)
	}
	if n != 3 {
		t.Fatalf("PurgeFinished removed %d, want 3 (closed-old, legacy-closed, expired-old)", n)
	}
	for _, key := range []string{closedOld, legacy, expiredOld} {
		if _, err := s.Get(key); !errors.Is(err, ErrNotFound) {
			t.Fatalf("purged poll %q: err %v, want ErrNotFound", key, err)
		}
	}
	for _, key := range []string{open, closedFresh, expiredFresh} {
		if _, err := s.Get(key); err != nil {
			t.Fatalf("surviving poll %q: %v", key, err)
		}
	}
}

// TestMaintenance pins the backend housekeeping: the sqlite checkpoint
// and compact run without error and the data survives both.
func TestMaintenance(t *testing.T) {
	s := openTestStore(t)
	key, err := s.Create(sampleMeeting())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, compact := range []bool{false, true} {
		if err := s.Maintenance(compact); err != nil {
			t.Fatalf("Maintenance(%v): %v", compact, err)
		}
	}
	if _, err := s.Get(key); err != nil {
		t.Fatalf("Get after maintenance: %v", err)
	}
}
