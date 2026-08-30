package producer

import (
	"fmt"
	"time"
)

// scheduleProducerMetadata is the config stored on the schedule producer worker row.
type scheduleProducerMetadata struct {
	PollRate time.Duration `json:"poll_rate"`
}

func (m *scheduleProducerMetadata) Validate() error {
	if m.PollRate <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate)
	}
	return nil
}

// ***************
// *** HELPERS ***
// ***************

// defaultScheduleProducerMetadata is the config the system's declaration starts with --
// anything needing sub-minute frequency stays a long-lived worker, so the
// scan paces at a minute.
func defaultScheduleProducerMetadata() *scheduleProducerMetadata {
	return &scheduleProducerMetadata{PollRate: time.Minute}
}
