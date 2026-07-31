package system

import (
	"fmt"
	"time"
)

// System is the singleton config row, read back for get/alter.
type System struct {
	Id                  int64
	AlertPollRate       time.Duration
	AlertRepeatInterval time.Duration
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func NewSystem(id int64, alertPollRate, alertRepeatInterval time.Duration, createdAt, updatedAt time.Time) (*System, error) {
	if id <= 0 {
		return nil, fmt.Errorf("id must be > 0, got %d", id)
	}
	if alertPollRate < 0 {
		return nil, fmt.Errorf("alertPollRate must be >= 0, got %v", alertPollRate)
	}
	if alertRepeatInterval < 0 {
		return nil, fmt.Errorf("alertRepeatInterval must be >= 0, got %v", alertRepeatInterval)
	}
	return &System{
		Id:                  id,
		AlertPollRate:       alertPollRate,
		AlertRepeatInterval: alertRepeatInterval,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}, nil
}
