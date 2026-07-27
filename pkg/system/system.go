package system

import (
	"fmt"
	"time"
)

// System is the singleton config row (id = 0), read back for get/alter.
type System struct {
	AdvisorPollRate        time.Duration
	AdvisoryRepeatInterval time.Duration
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func NewSystem(advisorPollRate, advisoryRepeatInterval time.Duration, createdAt, updatedAt time.Time) (*System, error) {
	if advisorPollRate < 0 {
		return nil, fmt.Errorf("advisorPollRate must be >= 0, got %v", advisorPollRate)
	}
	if advisoryRepeatInterval < 0 {
		return nil, fmt.Errorf("advisoryRepeatInterval must be >= 0, got %v", advisoryRepeatInterval)
	}
	return &System{
		AdvisorPollRate:        advisorPollRate,
		AdvisoryRepeatInterval: advisoryRepeatInterval,
		CreatedAt:              createdAt,
		UpdatedAt:              updatedAt,
	}, nil
}
