package system

import (
	"time"
)

// System is the singleton system row, read back for get/alter.
type System struct {
	Id        int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
