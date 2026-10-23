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

// Meeting is a meeting poll. It is stored under a random URL key
// (Key), which doubles as the primary key on the relational backends.
type Meeting struct {
	Key          string    `gorm:"primaryKey"`
	Title        string    `gorm:"not null"`
	Description  string    `gorm:"not null"`
	Email        string    `gorm:"not null"`
	HideUsers    bool      `gorm:"not null;default:false"`
	SingleSignup bool      `gorm:"not null;default:false"`
	Dates        TimeSlice `gorm:"not null"`
	// Users is not persisted yet — respondent sign-up does not exist
	// (see AGENTS.md), so GORM must not create a table or FK for it.
	Users *Users `gorm:"-"`
}

type Users struct {
	Name  string
	Dates []time.Time
}
