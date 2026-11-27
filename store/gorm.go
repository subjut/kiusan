package store

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

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
	// engine is the backend kind; Maintenance uses it for the
	// sqlite-only housekeeping.
	engine string
}

// openGorm connects to a relational backend and prepares the schema.
// engine is one of mysql, mariadb, postgres, sqlite; dsn is the
// connection string (assembled by the app's config layer).
func openGorm(engine, dsn string) (Store, error) {
	if dsn == "" {
		return nil, fmt.Errorf("store: %s backend needs a connection string", engine)
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
	if err := db.AutoMigrate(&database.Meeting{}, &database.Vote{}, &database.VoteImage{}); err != nil {
		if sqlDB, cerr := db.DB(); cerr == nil {
			_ = sqlDB.Close()
		}
		return nil, fmt.Errorf("store: migrate %s schema: %w", engine, err)
	}

	return &gormStore{db: db, engine: engine}, nil
}

// createEntity inserts m, minting its two link keys first: both the user
// and the admin key must be free in both key columns, so a key must never
// collide with any other entity's user or admin key. A collision —
// extremely unlikely — loops until both are unique, capped so a
// misclassified error can't loop forever.
func createEntity(db *gorm.DB, what string, m any, assign func(userKey, adminKey string)) (string, error) {
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
		var count int64
		res := db.Model(m).
			Where("key IN ? OR admin_key IN ?", []string{userKey, adminKey}, []string{userKey, adminKey}).
			Count(&count)
		if res.Error != nil {
			return "", res.Error
		}
		if count > 0 {
			continue
		}
		assign(userKey, adminKey)
		if err := db.Create(m).Error; err != nil {
			if isUniqueViolation(err) {
				// Lost a race against a concurrent create; try new keys.
				continue
			}
			return "", err
		}
		return userKey, nil
	}
	return "", fmt.Errorf("store: %s: too many key collisions", what)
}

func (g *gormStore) Create(m *database.Meeting) (string, error) {
	return createEntity(g.db, "create", m, func(userKey, adminKey string) {
		m.Key, m.AdminKey = userKey, adminKey
	})
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
			"closed":        m.Closed,
			"closed_at":     m.ClosedAt,
			"dates":         m.Dates,
			"final_slots":   m.FinalSlots,
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

// PurgeFinished deletes the finished meetings whose retention has passed:
// it scans only the candidates (closed or already expired), checks each
// row's deletion date in Go — the min/plus-months math stays out of the
// SQL, so the query stays portable across the backends — and removes the
// due rows through the regular Delete path (a meeting row carries all its
// answers, so deleting it purges everything).
func (g *gormStore) PurgeFinished(now time.Time, months int) (int, error) {
	var meetings []database.Meeting
	if err := g.db.Where("closed = ? OR expires <= ?", true, now).
		Find(&meetings).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, m := range meetings {
		if m.DeletionDate(months).After(now) {
			continue
		}
		if err := g.Delete(m.Key); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// Maintenance checkpoints the sqlite WAL on every call (the -wal file
// stays bounded between runs) and, while compact is set — right after a
// purge removed rows, when dead space appeared — also runs PRAGMA optimize
// and a VACUUM that rewrites the file and gives the freed pages back.
// The relational backends maintain themselves, so it is a no-op there.
func (g *gormStore) Maintenance(compact bool) error {
	if g.engine != BackendSQLite {
		return nil
	}
	if err := g.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return err
	}
	if !compact {
		return nil
	}
	if err := g.db.Exec("PRAGMA optimize").Error; err != nil {
		return err
	}
	// A VACUUM must run outside a transaction; the pooled connections
	// of this single-process app sit idle between queries, so it holds.
	return g.db.Exec("VACUUM").Error
}

func (g *gormStore) Close() error {
	sqlDB, err := g.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// ---- VoteStore ----

func (g *gormStore) CreateVote(v *database.Vote) (string, error) {
	return createEntity(g.db, "create vote", v, func(userKey, adminKey string) {
		v.Key, v.AdminKey = userKey, adminKey
	})
}

func (g *gormStore) GetVote(key string) (*database.Vote, error) {
	var v database.Vote
	res := g.db.Where("key = ?", key).Take(&v)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return &v, nil
}

func (g *gormStore) GetVoteAdmin(key string) (*database.Vote, error) {
	var v database.Vote
	res := g.db.Where("admin_key = ?", key).Take(&v)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return &v, nil
}

func (g *gormStore) UpdateVote(v *database.Vote) error {
	res := g.db.Model(&database.Vote{}).
		Where("key = ? AND generation = ?", v.Key, v.Generation).
		Updates(map[string]any{
			"title":         v.Title,
			"description":   v.Description,
			"email":         v.Email,
			"hide_users":    v.HideUsers,
			"single_choice": v.SingleChoice,
			"must_answer":   v.MustAnswer,
			"hide_totals":   v.HideTotals,
			"vote_type":     v.VoteType,
			"closed":        v.Closed,
			"closed_at":     v.ClosedAt,
			"choices":       v.Choices,
			"final_choices": v.FinalChoices,
			"expires":       v.Expires,
			"answers":       v.Answers,
			"generation":    gorm.Expr("generation + 1"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrConflict
	}
	v.Generation++
	return nil
}

// DeleteVote removes the vote stored under key — and with it every image
// its options carry (the janitor is the backstop for the abandoned-upload
// case, not for a deleted vote).
func (g *gormStore) DeleteVote(key string) error {
	var v database.Vote
	if err := g.db.Where("key = ?", key).Take(&v).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if ids := voteImageIDs(v.Choices); len(ids) > 0 {
		if err := g.DeleteVoteImages(ids); err != nil {
			return err
		}
	}
	return g.db.Where("key = ?", key).Delete(&database.Vote{}).Error
}

// voteImageIDs collects the distinct image IDs a choice list names.
func voteImageIDs(cs database.ChoiceList) []string {
	seen := make(map[string]struct{}, len(cs))
	var ids []string
	for i := range cs {
		if cs[i].Image == "" {
			continue
		}
		if _, dup := seen[cs[i].Image]; dup {
			continue
		}
		seen[cs[i].Image] = struct{}{}
		ids = append(ids, cs[i].Image)
	}
	return ids
}

// PutVoteImage stores both sizes of one option image under id.
func (g *gormStore) PutVoteImage(id string, thumb, full []byte, created time.Time) error {
	rows := []database.VoteImage{
		{ID: id, Kind: database.VoteImageKindThumb, Data: thumb, Created: created},
		{ID: id, Kind: database.VoteImageKindFull, Data: full, Created: created},
	}
	return g.db.Create(&rows).Error
}

func (g *gormStore) GetVoteImage(id, kind string) ([]byte, error) {
	var img database.VoteImage
	res := g.db.Where("id = ? AND kind = ?", id, kind).Take(&img)
	if errors.Is(res.Error, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if res.Error != nil {
		return nil, res.Error
	}
	return img.Data, nil
}

// DeleteVoteImages removes both sizes of every image in ids; an empty
// list is a no-op.
func (g *gormStore) DeleteVoteImages(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	return g.db.Where("id IN ?", ids).Delete(&database.VoteImage{}).Error
}

// VoteImageInfos lists every stored image — one entry per ID (the thumb
// row is always stored, so it carries the ID's upload moment).
func (g *gormStore) VoteImageInfos() ([]VoteImageInfo, error) {
	var rows []database.VoteImage
	if err := g.db.Where("kind = ?", database.VoteImageKindThumb).
		Select("id", "created").Find(&rows).Error; err != nil {
		return nil, err
	}
	infos := make([]VoteImageInfo, 0, len(rows))
	for _, r := range rows {
		infos = append(infos, VoteImageInfo{ID: r.ID, Created: r.Created})
	}
	return infos, nil
}

// ReferencedVoteImageIDs returns the set of image IDs the stored votes'
// choices currently name. The choices are a JSON document in a TEXT
// column, so the set is built in Go — the query stays portable.
func (g *gormStore) ReferencedVoteImageIDs() (map[string]struct{}, error) {
	var votes []database.Vote
	if err := g.db.Find(&votes).Error; err != nil {
		return nil, err
	}
	refs := make(map[string]struct{})
	for i := range votes {
		for _, id := range voteImageIDs(votes[i].Choices) {
			refs[id] = struct{}{}
		}
	}
	return refs, nil
}

// PurgeFinishedVote is the vote's counterpart of PurgeFinished: finished
// (closed or expired) candidates, per-row deletion-date check in Go, due
// rows removed through the regular DeleteVote path.
func (g *gormStore) PurgeFinishedVote(now time.Time, months int) (int, error) {
	var votes []database.Vote
	if err := g.db.Where("closed = ? OR expires <= ?", true, now).
		Find(&votes).Error; err != nil {
		return 0, err
	}
	n := 0
	for _, v := range votes {
		if v.DeletionDate(months).After(now) {
			continue
		}
		if err := g.DeleteVote(v.Key); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
