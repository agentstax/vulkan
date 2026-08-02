package system

import (
	"fmt"
	"time"
)

// System is the singleton config row, read back for get/alter.
type System struct {
	Id                  int64
	AlertRepeatInterval time.Duration
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func NewSystem(id int64, alertRepeatInterval time.Duration, createdAt, updatedAt time.Time) (*System, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id must be > 0, got %d", id)
	}
	if alertRepeatInterval < 0 {
		return nil, fmt.Errorf("alertRepeatInterval must be >= 0, got %v", alertRepeatInterval)
	}
	return &System{
		Id:                  id,
		AlertRepeatInterval: alertRepeatInterval,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}, nil
}
