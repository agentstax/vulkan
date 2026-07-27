package system

import (
	"fmt"
	"time"
)

// System is the singleton config row (id = 0), read back for get/alter.
type System struct {
	AlertPollRate       time.Duration
	AlertRepeatInterval time.Duration
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func NewSystem(alertPollRate, alertRepeatInterval time.Duration, createdAt, updatedAt time.Time) (*System, error) {
	if alertPollRate < 0 {
		return nil, fmt.Errorf("alertPollRate must be >= 0, got %v", alertPollRate)
	}
	if alertRepeatInterval < 0 {
		return nil, fmt.Errorf("alertRepeatInterval must be >= 0, got %v", alertRepeatInterval)
	}
	return &System{
		AlertPollRate:       alertPollRate,
		AlertRepeatInterval: alertRepeatInterval,
		CreatedAt:           createdAt,
		UpdatedAt:           updatedAt,
	}, nil
}
