package store

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/glebarez/sqlite"
	"github.com/subjut/kiusan/database"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

// gormStore implements MeetingStore on top of a GORM database. One
// dialector serves all relational backends: mysql (and mariadb), postgres,
// and the pure-Go sqlite driver.
type gormStore struct {
	db *gorm.DB
}

// openGorm connects to a relational backend and prepares the schema.
// engine is one of mysql, mariadb, postgres, sqlite; dsn is the
// connection string (KIUSAN_DSN).
func openGorm(engine, dsn string) (MeetingStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("store: %s backend needs a connection string (set KIUSAN_DSN)", engine)
	}

	var dialector gorm.Dialector
	switch engine {
	case "mysql", "mariadb":
		dialector = mysql.Open(dsn)
	case "postgres":
		dialector = postgres.Open(dsn)
	case "sqlite":
		dialector = sqlite.Open(dsn)
	default:
		return nil, fmt.Errorf("store: unknown GORM engine %q", engine)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: gormlogger.Default.LogMode(gormlogger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", engine, err)
	}

	// The sqlite data file holds the admin capability keys; keep it
	// private once it exists (non-fatal if it can't be chmodded, e.g.
	// a :memory: DSN).
	if engine == BackendSQLite {
		if cerr := os.Chmod(dsn, 0o600); cerr != nil {
			log.Printf("store: could not chmod %s: %v", dsn, cerr)
		}
	}

	// AutoMigrate keeps the schema in sync at startup. It is additive-only:
	// column additions and new tables. Destructive changes need explicit
	// SQL migrations (none exist yet).
	if err := db.AutoMigrate(&database.Meeting{}); err != nil {
		if sqlDB, cerr := db.DB(); cerr == nil {
			_ = sqlDB.Close()
		}
		return nil, fmt.Errorf("store: migrate %s schema: %w", engine, err)
	}

	return &gormStore{db: db}, nil
}

func (g *gormStore) Create(m *database.Meeting) (string, error) {
	// Cap the collision retries so a misclassified error can't loop forever.
	const maxAttempts = 10
	for attempt := 0; attempt < maxAttempts; attempt++ {
		userKey, err := RandomKey(16)
		if err != nil {
			return "", err
		}
		adminKey, err := RandomKey(16)
		if err != nil {
			return "", err
		}
		// Both keys must be free in both key columns: a key must never
		// collide with any other meeting's user or admin key.
		var count int64
		res := g.db.Model(&database.Meeting{}).
			Where("key IN ? OR admin_key IN ?", []string{userKey, adminKey}, []string{userKey, adminKey}).
			Count(&count)
		if res.Error != nil {
			return "", res.Error
		}
		if count > 0 {
			// Collision: extremely unlikely, but loop until both are unique.
			continue
		}
		m.Key, m.AdminKey = userKey, adminKey
		if err := g.db.Create(m).Error; err != nil {
			if isUniqueViolation(err) {
				// Lost a race against a concurrent create; try new keys.
				continue
			}
			return "", err
		}
		return userKey, nil
	}
	return "", fmt.Errorf("store: create: too many key collisions")
}

// isUniqueViolation recognizes duplicate-key/unique errors from the
// supported engines (mysql/mariadb/postgres/sqlite). It deliberately
// does not match the generic word "constraint", so a NOT NULL
// violation can't be mistaken for a key collision.
func isUniqueViolation(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "duplicate") ||
		strings.Contains(s, "unique")
}

func (g *gormStore) Get(key string) (*database.Meeting, error) {
	var meeting database.Meeting
	res := g.db.Where("key = ?", key).Take(&meeting)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return &meeting, nil
}

func (g *gormStore) GetAdmin(key string) (*database.Meeting, error) {
	var meeting database.Meeting
	res := g.db.Where("admin_key = ?", key).Take(&meeting)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return &meeting, nil
}

// Update re-saves all mutable fields under a compare-and-set on the
// meeting's Generation: it only lands if nobody else wrote in between.
func (g *gormStore) Update(m *database.Meeting) error {
	res := g.db.Model(&database.Meeting{}).
		Where("key = ? AND generation = ?", m.Key, m.Generation).
		Updates(map[string]any{
			"admin_key":     m.AdminKey,
			"title":         m.Title,
			"description":   m.Description,
			"email":         m.Email,
			"hide_users":    m.HideUsers,
			"single_signup": m.SingleSignup,
			"dates":         m.Dates,
			"time_zone":     m.TimeZone,
			"expires":       m.Expires,
			"answers":       m.Answers,
			"generation":    gorm.Expr("generation + 1"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrConflict
	}
	// Keep the caller's copy in sync with what was stored.
	m.Generation++
	return nil
}

func (g *gormStore) Delete(key string) error {
	return g.db.Where("key = ?", key).Delete(&database.Meeting{}).Error
}

func (g *gormStore) Close() error {
	sqlDB, err := g.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
