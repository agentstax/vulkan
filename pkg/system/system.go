package system

import (
	"time"
)

// System is the singleton config row, read back for get/alter.
type System struct {
	Id                  int64
	AlertRepeatInterval time.Duration
	CreatedAt           time.Time
	UpdatedAt           time.Time
}
