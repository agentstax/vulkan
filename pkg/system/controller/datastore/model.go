package datastore

import (
	"time"
)

// SystemConfigRow models the system_config table row exactly.
type SystemConfigRow struct {
	Id        int64     `db:"id"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
}
