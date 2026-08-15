package datastore

import (
	"time"
)

// SystemData models the system table row exactly.
type SystemData struct {
	Id        int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
