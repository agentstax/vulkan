package cronscheduler

import (
	"fmt"
	"time"
)

// cronSchedulerMetadata is the config stored on the cron scheduler worker row.
type cronSchedulerMetadata struct {
	PollRate time.Duration `json:"poll_rate"`
}

// defaultCronSchedulerMetadata is the config the system's declaration starts with --
// anything needing sub-minute frequency stays a long-lived worker, so the
// scan paces at a minute.
func defaultCronSchedulerMetadata() *cronSchedulerMetadata {
	return &cronSchedulerMetadata{PollRate: time.Minute}
}

func (m *cronSchedulerMetadata) Validate() error {
	if m.PollRate <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate)
	}
	return nil
}
