package store

import (
	"errors"
	"fmt"

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
	for {
		key, err := RandomKey(16)
		if err != nil {
			return "", err
		}
		var existing database.Meeting
		res := g.db.Where("key = ?", key).Take(&existing)
		if res.Error == nil {
			// Collision: extremely unlikely, but loop until it is unique.
			continue
		}
		if !errors.Is(res.Error, gorm.ErrRecordNotFound) {
			return "", res.Error
		}
		m.Key = key
		if err := g.db.Create(m).Error; err != nil {
			return "", err
		}
		return key, nil
	}
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
