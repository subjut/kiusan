package store

import "github.com/subjut/kiusan/database"

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
}
