package database

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// Choice is one option in a vote. ID is a stable identifier minted when
// the option is created: answers are keyed by it, so reordering the
// options never shifts a stored answer, and deleting one drops exactly
// its answers. Editing an option's text counts as a new option (a fresh
// ID), which prunes the old option's answers. Text is the choice's
// label; Image is reserved for the future drag-and-drop upload (empty
// for now).
type Choice struct {
	ID    string `json:"id"`
	Text  string `json:"text"`
	Image string `json:"image,omitempty"`
}

// ChoiceList holds a vote's choices. It persists as a JSON document in a
// single TEXT column (via GormDataType/Value/Scan) like TimeSlice, so the
// schema stays portable across every backend.
type ChoiceList []Choice

// GormDataType tells GORM to use a plain text column.
func (ChoiceList) GormDataType() string { return "text" }

// Value implements driver.Valuer; GORM calls this before storing.
func (c ChoiceList) Value() (driver.Value, error) {
	if c == nil {
		return "[]", nil
	}
	b, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner; GORM calls this when reading.
func (c *ChoiceList) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*c = nil
		return nil
	case []byte:
		return json.Unmarshal(v, c)
	case string:
		return json.Unmarshal([]byte(v), c)
	default:
		return fmt.Errorf("database: cannot scan %T into ChoiceList", value)
	}
}

// VoteAnswer is one respondent's answer to a vote. In mark mode, Marks
// maps choice ID to "y", "m", or "n". In free-text mode, Notes maps
// choice ID to the voter's text. Only one of the two maps is populated,
// determined by Vote.FreeText.
type VoteAnswer struct {
	ID        string
	Name      string
	Marks     map[string]string
	Notes     map[string]string
	UpdatedAt time.Time
}

// VoteAnswerList holds a vote's answers. It persists as a JSON document
// in a single TEXT column (via GormDataType/Value/Scan) like AnswerList,
// so the schema stays portable across every backend.
type VoteAnswerList []VoteAnswer

// GormDataType tells GORM to use a plain text column.
func (VoteAnswerList) GormDataType() string { return "text" }

// Value implements driver.Valuer; GORM calls this before storing.
func (v VoteAnswerList) Value() (driver.Value, error) {
	if v == nil {
		return "[]", nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner; GORM calls this when reading.
func (v *VoteAnswerList) Scan(value any) error {
	switch val := value.(type) {
	case nil:
		*v = nil
		return nil
	case []byte:
		if len(val) == 0 {
			*v = nil
			return nil
		}
		return json.Unmarshal(val, v)
	case string:
		if val == "" {
			*v = nil
			return nil
		}
		return json.Unmarshal([]byte(val), v)
	default:
		return fmt.Errorf("database: cannot scan %T into VoteAnswerList", value)
	}
}

// Vote is a choice poll. It is stored under two random URL keys:
// Key (the user/respondent link, primary key) and AdminKey (the admin
// link). Both are created with the vote and never change, so editing the
// vote never invalidates a shared link.
type Vote struct {
	Key         string `gorm:"primaryKey"`
	AdminKey    string `gorm:"not null;uniqueIndex"`
	Title       string `gorm:"not null"`
	Description string `gorm:"not null"`
	Email       string `gorm:"not null"`
	HideUsers   bool   `gorm:"not null;default:false"`
	// SingleChoice limits a voter to one "y" mark (mark mode) or one
	// non-empty note (free-text mode).
	SingleChoice bool `gorm:"not null;default:false"`
	// FreeText switches the answer mode: when true, voters write a text
	// per choice instead of marking yes/maybe/no.
	FreeText bool `gorm:"not null;default:false"`
	// Closed stops a vote from accepting answers before its end date.
	Closed  bool           `gorm:"not null;default:false"`
	Choices ChoiceList     `gorm:"not null"`
	Expires time.Time      `gorm:"not null"`
	Answers VoteAnswerList `gorm:"not null;default:'[]'"`
	// Generation advances with every store-level write; it is the
	// compare-and-set stamp of VoteStore.UpdateVote.
	Generation int `gorm:"not null;default:0"`
}
