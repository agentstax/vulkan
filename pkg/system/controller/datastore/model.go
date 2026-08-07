package datastore

import (
	"time"
)

// SystemData models the system table row exactly.
type SystemData struct {
	Id                    int64
	AlertRepeatIntervalNs int64
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

// AlterSystemData is UpdateSystem's sparse patch -- a nil field means leave
// the column unchanged.
type AlterSystemData struct {
	AlertRepeatIntervalNs *int64
}
