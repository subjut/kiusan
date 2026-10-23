// Package store is the persistence layer of kiusan. Handlers talk to
// storage only through the MeetingStore interface, so the backend can be
// swapped without touching request code.
//
// Supported backends (selected via the KIUSAN_STORE environment variable):
//
//   - "pudge" (or empty): the embedded Pudge key-value database. Data lives
//     in the `meetings` and `meetings.idx` files in the working directory.
//     This is the default and needs no configuration.
//   - "mysql" / "mariadb": GORM + go-sql-driver/mysql.
//     KIUSAN_DSN, e.g. "user:pass@tcp(localhost:3306)/kiusan"
//   - "postgres": GORM + pgx.
//     KIUSAN_DSN, e.g. "postgres://user:pass@localhost:5432/kiusan?sslmode=disable"
//   - "sqlite": GORM + pure-Go sqlite (no cgo).
//     KIUSAN_DSN defaults to "./meetings.db"
package store

import (
	"errors"
	"fmt"

	"github.com/subjut/kiusan/database"
)

// Backend names accepted by Open (and in KIUSAN_STORE).
const (
	BackendPuge     = "pudge"
	BackendMySQL    = "mysql"
	BackendMariaDB  = "mariadb"
	BackendPostgres = "postgres"
	BackendSQLite   = "sqlite"
)

// ErrNotFound is returned by MeetingStore.Get when no meeting is stored
// under the given key.
var ErrNotFound = errors.New("store: key not found")

// MeetingStore is the storage contract for meetings.
type MeetingStore interface {
	// Create stores the meeting and returns the assigned URL key.
	Create(m *database.Meeting) (string, error)
	// Get returns the meeting stored under key, or ErrNotFound if absent.
	Get(key string) (*database.Meeting, error)
	// Delete removes the meeting stored under key.
	Delete(key string) error
	// Close releases the backend's resources.
	Close() error
}

// Default data-file names, used when KIUSAN_DSN is not set.
const (
	defaultPudgeName  = "meetings"
	defaultSqliteName = "meetings.db"
)

// Open returns a MeetingStore for the given backend kind. An empty kind
// selects the default Pudge backend; the relational backends need a DSN
// (sqlite falls back to defaultSqliteName).
func Open(kind, dsn string) (MeetingStore, error) {
	switch kind {
	case "", BackendPuge:
		if dsn == "" {
			dsn = defaultPudgeName
		}
		return newPugeStore(dsn)
	case BackendMySQL, BackendMariaDB, BackendPostgres, BackendSQLite:
		if dsn == "" && kind == BackendSQLite {
			dsn = defaultSqliteName
		}
		return openGorm(kind, dsn)
	default:
		return nil, fmt.Errorf("store: unknown backend %q (valid: %s, %s, %s, %s, %s)",
			kind, BackendPuge, BackendMySQL, BackendMariaDB, BackendPostgres, BackendSQLite)
	}
}
