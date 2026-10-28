package database

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// TimeSlice holds the candidate dates of a meeting. It persists as a JSON
// document in a single TEXT column (via GormDataType/Value/Scan), so the
// schema stays portable across every backend.
type TimeSlice []time.Time

// GormDataType tells GORM to use a plain text column.
func (TimeSlice) GormDataType() string { return "text" }

// Value implements driver.Valuer; GORM calls this before storing.
func (t TimeSlice) Value() (driver.Value, error) {
	if t == nil {
		return "[]", nil
	}
	b, err := json.Marshal(t)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner; GORM calls this when reading.
func (t *TimeSlice) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*t = nil
		return nil
	case []byte:
		return json.Unmarshal(v, t)
	case string:
		return json.Unmarshal([]byte(v), t)
	default:
		return fmt.Errorf("database: cannot scan %T into TimeSlice", value)
	}
}

// Answer is one respondent's answer to a poll: an opaque identity token
// (ID, the identity cookie value), a display name, and the marked slots.
// Marks is keyed by the slot's absolute instant and holds "g" (available)
// or "y" (maybe); a missing key means "no" (red X).
type Answer struct {
	ID        string
	Name      string
	Marks     map[string]string
	UpdatedAt time.Time
}

// AnswerList holds a poll's answers. It persists as a JSON document in a
// single TEXT column (via GormDataType/Value/Scan) like TimeSlice, so the
// schema stays portable across every backend; on the Pudge backend it is
// simply gob-encoded inside the meeting value.
type AnswerList []Answer

// GormDataType tells GORM to use a plain text column.
func (AnswerList) GormDataType() string { return "text" }

// Value implements driver.Valuer; GORM calls this before storing.
func (a AnswerList) Value() (driver.Value, error) {
	if a == nil {
		return "[]", nil
	}
	b, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner; GORM calls this when reading.
func (a *AnswerList) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*a = nil
		return nil
	case []byte:
		if len(v) == 0 {
			*a = nil
			return nil
		}
		return json.Unmarshal(v, a)
	case string:
		if v == "" {
			*a = nil
			return nil
		}
		return json.Unmarshal([]byte(v), a)
	default:
		return fmt.Errorf("database: cannot scan %T into AnswerList", value)
	}
}

// Meeting is a meeting poll. It is stored under two random URL keys:
// Key (the user/respondent link, primary key on the relational backends)
// and AdminKey (the admin link). Both are created with the poll and never
// change, so editing the poll never invalidates a shared link.
type Meeting struct {
	Key string `gorm:"primaryKey"`
	// AdminKey is stored NULL on relational backends when empty (legacy
	// polls predate the admin link); a unique index allows many NULLs, which
	// a unique-on-empty-string could not.
	AdminKey     string    `gorm:"uniqueIndex"`
	Title        string    `gorm:"not null"`
	Description  string    `gorm:"not null"`
	Email        string    `gorm:"not null"`
	HideUsers    bool      `gorm:"not null;default:false"`
	SingleSignup bool      `gorm:"not null;default:false"`
	Dates        TimeSlice `gorm:"not null"`
	// TimeZone is the IANA zone (e.g. "Europe/Berlin") the meeting's
	// dates and times are authored in; slots are absolute instants,
	// rendered in this zone. Empty = legacy, rendered as UTC.
	TimeZone string `gorm:"not null;default:''"`
	// Expires is when the poll stops accepting answers (end of the chosen
	// day in TimeZone); nil = no end (legacy polls).
	Expires *time.Time
	// Answers are the submitted answers (identity, name, marks per slot).
	Answers AnswerList `gorm:"not null;default:'[]'"`
	// Generation advances with every store-level write; it is the
	// compare-and-set stamp of MeetingStore.Update.
	Generation int `gorm:"not null;default:0"`
}
