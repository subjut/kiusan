package database

import (
	"time"

	"github.com/recoilme/pudge"
)

type Meeting struct {
	Title        string
	Description  string
	Email        string
	HideUsers    bool
	SingleSignup bool
	Dates        []time.Time
	Users        *Users
}

type Users struct {
	Name  string
	Dates []time.Time
}

var (
	DB *pudge.Db
)

const MeetingDb = "meetings"
const UserDb = "users"
