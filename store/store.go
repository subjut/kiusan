// Package store is the persistence layer of kiusan. Handlers talk to
// storage only through the MeetingStore interface, so the backend can be
// swapped without touching request code.
//
// Supported backends (selected via the KIUSAN_STORE environment variable):
//
//   - "sqlite" (or empty): GORM + pure-Go sqlite (no cgo). This is the
//     default and needs no configuration; KIUSAN_DSN defaults to
//     "./meetings.db".
//   - "mysql" / "mariadb": GORM + go-sql-driver/mysql.
//     KIUSAN_DSN, e.g. "user:pass@tcp(localhost:3306)/kiusan"
//   - "postgres": GORM + pgx.
//     KIUSAN_DSN, e.g. "postgres://user:pass@localhost:5432/kiusan?sslmode=disable"
package store

import (
	"errors"
	"fmt"

	"github.com/subjut/kiusan/database"
)

// Backend names accepted by Open (and in KIUSAN_STORE).
const (
	BackendMySQL    = "mysql"
	BackendMariaDB  = "mariadb"
	BackendPostgres = "postgres"
	BackendSQLite   = "sqlite"
)

// ErrNotFound is returned by MeetingStore.Get/GetAdmin when no meeting is
// stored under the given key.
var ErrNotFound = errors.New("store: key not found")

// ErrConflict is returned by MeetingStore.Update when the stored meeting's
// Generation no longer matches m.Generation (a concurrent write happened).
var ErrConflict = errors.New("store: update conflict: stale generation")

// MeetingStore is the storage contract for meetings. A meeting is stored
// under two random keys: its user link key (the primary key) and its admin
// link key.
type MeetingStore interface {
	// Create stores the meeting, assigns both of its random keys and
	// returns the user link key.
	Create(m *database.Meeting) (string, error)
	// Get returns the meeting stored under the user link key, or
	// ErrNotFound if absent.
	Get(key string) (*database.Meeting, error)
	// GetAdmin returns the meeting stored under the admin link key, or
	// ErrNotFound if absent.
	GetAdmin(key string) (*database.Meeting, error)
	// Update writes m back, but only if the stored meeting still has
	// Generation == m.Generation; the stored Generation then advances by
	// one (mirrored onto m). It returns ErrConflict otherwise.
	Update(m *database.Meeting) error
	// Delete removes the meeting stored under key.
	Delete(key string) error
	// Close releases the backend's resources.
	Close() error
}

// defaultSqliteName is the default data-file name, used when KIUSAN_DSN is
// not set for the default sqlite backend.
const defaultSqliteName = "meetings.db"

// Open returns a MeetingStore for the given backend kind. An empty kind
// selects the default sqlite backend; mysql, mariadb and postgres need a
// DSN (sqlite falls back to defaultSqliteName).
func Open(kind, dsn string) (MeetingStore, error) {
	switch kind {
	case "", BackendSQLite:
		if dsn == "" {
			dsn = defaultSqliteName
		}
		return openGorm(BackendSQLite, dsn)
	case BackendMySQL, BackendMariaDB, BackendPostgres:
		return openGorm(kind, dsn)
	default:
		return nil, fmt.Errorf("store: unknown backend %q (valid: %s, %s, %s, %s)",
			kind, BackendMySQL, BackendMariaDB, BackendPostgres, BackendSQLite)
	}
}
