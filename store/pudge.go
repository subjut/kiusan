package store

import (
	"sync"

	"github.com/recoilme/pudge"

	"github.com/subjut/kiusan/database"
)

// adminPrefix namespaces the admin-key → user-key pointer entries that
// make admin links resolvable in the flat key-value store.
const adminPrefix = "admin:"

// pudgeStore implements MeetingStore on top of the embedded Pudge
// key-value database (the default backend).
type pudgeStore struct {
	db *pudge.Db
	// mu serializes compare-and-set updates; the embedded database is
	// single-process, so an in-process lock makes Update atomic.
	mu sync.Mutex
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
		userKey, err := RandomKey(16)
		if err != nil {
			return "", err
		}
		adminKey, err := RandomKey(16)
		if err != nil {
			return "", err
		}
		// Both keys must be free as user keys and as admin keys: a key
		// must never collide with any other meeting's user or admin key.
		busy, err := s.keyTaken(userKey)
		if err != nil {
			return "", err
		}
		if !busy {
			busy, err = s.keyTaken(adminKey)
			if err != nil {
				return "", err
			}
		}
		if busy {
			// Collision: extremely unlikely, but loop until both are unique.
			continue
		}
		m.Key, m.AdminKey = userKey, adminKey
		if err := s.db.Set(userKey, m); err != nil {
			return "", err
		}
		if err := s.db.Set(adminPrefix+adminKey, userKey); err != nil {
			_ = s.db.Delete(userKey) // do not leak the half-written meeting
			return "", err
		}
		return userKey, nil
	}
}

// keyTaken reports whether key exists as a meeting user key or as an admin
// pointer.
func (s *pudgeStore) keyTaken(key string) (bool, error) {
	has, err := s.db.Has(key)
	if err != nil {
		return false, err
	}
	if has {
		return true, nil
	}
	return s.db.Has(adminPrefix + key)
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

func (s *pudgeStore) GetAdmin(adminKey string) (*database.Meeting, error) {
	userKey := new(string)
	has, err := s.db.Has(adminPrefix + adminKey)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, ErrNotFound
	}
	if err := s.db.Get(adminPrefix+adminKey, userKey); err != nil {
		return nil, err
	}
	return s.Get(*userKey)
}

// Update re-saves the meeting under a compare-and-set on its Generation:
// it only lands if the stored copy still has the Generation the caller
// last read (serialized on the process mutex, the store's single-writer).
func (s *pudgeStore) Update(m *database.Meeting) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	stored := new(database.Meeting)
	has, err := s.db.Has(m.Key)
	if err != nil {
		return err
	}
	if !has {
		return ErrConflict
	}
	if err := s.db.Get(m.Key, stored); err != nil {
		return err
	}
	if stored.Generation != m.Generation {
		return ErrConflict
	}
	m.Generation++
	if err := s.db.Set(m.Key, m); err != nil {
		return err
	}
	// The admin pointer must follow the stored meeting when the admin key
	// changed (writing it away entirely downgrades the poll to legacy).
	if stored.AdminKey != m.AdminKey {
		if stored.AdminKey != "" {
			if err := s.db.Delete(adminPrefix + stored.AdminKey); err != nil {
				return err
			}
		}
		if m.AdminKey != "" {
			if err := s.db.Set(adminPrefix+m.AdminKey, m.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *pudgeStore) Delete(key string) error {
	m := new(database.Meeting)
	has, err := s.db.Has(key)
	if err != nil {
		return err
	}
	if !has {
		return ErrNotFound
	}
	if err := s.db.Get(key, m); err != nil {
		return err
	}
	if err := s.db.Delete(key); err != nil {
		return err
	}
	if m.AdminKey != "" {
		return s.db.Delete(adminPrefix + m.AdminKey)
	}
	return nil
}

func (s *pudgeStore) Close() error {
	return s.db.Close()
}
