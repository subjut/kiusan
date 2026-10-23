package store

import (
	"github.com/recoilme/pudge"

	"github.com/subjut/kiusan/database"
)

// pudgeStore implements MeetingStore on top of the embedded Pudge
// key-value database (the default backend).
type pudgeStore struct {
	db *pudge.Db
}

func newPugeStore(name string) (MeetingStore, error) {
	db, err := pudge.Open(name, nil)
	if err != nil {
		return nil, err
	}
	return &pudgeStore{db: db}, nil
}

func (s *pudgeStore) Create(m *database.Meeting) (string, error) {
	for {
		key, err := RandomKey(16)
		if err != nil {
			return "", err
		}
		has, err := s.db.Has(key)
		if err != nil {
			return "", err
		}
		if has {
			// Collision: extremely unlikely, but loop until it is unique.
			continue
		}
		m.Key = key
		if err := s.db.Set(key, m); err != nil {
			return "", err
		}
		return key, nil
	}
}

func (s *pudgeStore) Get(key string) (*database.Meeting, error) {
	has, err := s.db.Has(key)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	m := new(database.Meeting)
	if err := s.db.Get(key, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *pudgeStore) Delete(key string) error {
	return s.db.Delete(key)
}

func (s *pudgeStore) Close() error {
	return s.db.Close()
}
