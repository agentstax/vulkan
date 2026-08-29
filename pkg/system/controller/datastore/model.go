package datastore

import (
	"time"
)

// SystemData models the system_config table row exactly.
type SystemData struct {
	Id        int64     `db:"id"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}
