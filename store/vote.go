package store

import (
	"time"

	"github.com/subjut/kiusan/database"
)

// VoteImageInfo is one stored option image (one entry per ID, with the
// moment it was uploaded) — the janitor's unreferenced sweep input.
type VoteImageInfo struct {
	ID      string
	Created time.Time
}

// VoteStore is the storage contract for votes. A vote is stored under two
// random keys (user link and admin link), like MeetingStore.
type VoteStore interface {
	// CreateVote stores the vote, assigns both of its random keys and
	// returns the user link key.
	CreateVote(v *database.Vote) (string, error)
	// GetVote returns the vote stored under the user link key, or
	// ErrNotFound if absent.
	GetVote(key string) (*database.Vote, error)
	// GetVoteAdmin returns the vote stored under the admin link key, or
	// ErrNotFound if absent.
	GetVoteAdmin(key string) (*database.Vote, error)
	// UpdateVote writes v back, but only if the stored vote still has
	// Generation == v.Generation; the stored Generation then advances by
	// one (mirrored onto v). It returns ErrConflict otherwise.
	UpdateVote(v *database.Vote) error
	// DeleteVote removes the vote stored under key.
	DeleteVote(key string) error
	// PurgeFinishedVote deletes the finished votes — closed or past
	// their end date — that finished at least months ago (calendar
	// months after the earlier of closure and end date), and returns
	// how many it removed.
	PurgeFinishedVote(now time.Time, months int) (int, error)

	// PutVoteImage stores both sizes of one option image under id
	// (the ID the caller minted; it is the value Choice.Image carries).
	PutVoteImage(id string, thumb, full []byte, created time.Time) error
	// GetVoteImage returns the stored bytes of one size of the image
	// under id, or ErrNotFound if absent.
	GetVoteImage(id, kind string) ([]byte, error)
	// DeleteVoteImages removes both sizes of every image in ids.
	DeleteVoteImages(ids []string) error
	// VoteImageInfos lists every stored image (one entry per ID, with
	// its upload moment).
	VoteImageInfos() ([]VoteImageInfo, error)
	// ReferencedVoteImageIDs returns the set of image IDs the stored
	// votes' choices currently name.
	ReferencedVoteImageIDs() (map[string]struct{}, error)
}
