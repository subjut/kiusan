package main

import (
	"log"
	"time"

	"github.com/subjut/kiusan/store"
)

// janitorInterval is the maintenance tick: a finished poll's or vote's
// row is checked against its deletion date at most this often, so the
// worst case keeps a due row an extra day past its deadline.
const janitorInterval = 24 * time.Hour

// purgeFinished runs one maintenance pass: it purges the finished polls
// and votes whose retention (finished + months, the earlier of closure
// and end date) has passed, then checkpoints the backend — compacting
// the sqlite file only while this pass removed something, which is when
// dead space appears. A backend error is logged, not returned: one bad
// pass must not stop the loop, the next tick retries.
func purgeFinished(s store.Store, now time.Time, months int) (int, int) {
	purged, err := s.PurgeFinished(now, months)
	if err != nil {
		log.Printf("maintenance: purging finished polls: %v", err)
	}
	purgedVotes, err := s.PurgeFinishedVote(now, months)
	if err != nil {
		log.Printf("maintenance: purging finished votes: %v", err)
	}
	if err := s.Maintenance(purged+purgedVotes > 0); err != nil {
		log.Printf("maintenance: housekeeping: %v", err)
	}
	if purged+purgedVotes > 0 {
		log.Printf("maintenance: purged %d finished poll(s), %d finished vote(s)",
			purged, purgedVotes)
	}
	return purged, purgedVotes
}

// runJanitor is the maintenance loop: one pass at start, then every
// janitorInterval.
func runJanitor(s store.Store, months int) {
	go func() {
		purgeFinished(s, time.Now(), months)
		ticker := time.NewTicker(janitorInterval)
		defer ticker.Stop()
		for range ticker.C {
			purgeFinished(s, time.Now(), months)
		}
	}()
}
