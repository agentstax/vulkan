package janitor

import (
	"fmt"
	"time"
)

// janitorMetadata is the worker row's own tuning.
type janitorMetadata struct {
	PollRate       time.Duration `json:"poll_rate"`
	SweepBatchSize int           `json:"sweep_batch_size"` // rows deleted per sweep transaction
}

// defaultJanitorMetadata is the tuning every topic's seed starts with.
func defaultJanitorMetadata() *janitorMetadata {
	return &janitorMetadata{PollRate: 5 * time.Second, SweepBatchSize: 1000}
}

func (m *janitorMetadata) Validate() error {
	if m.PollRate <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate)
	}
	if m.SweepBatchSize <= 0 {
		return fmt.Errorf("sweep_batch_size must be > 0, got %d", m.SweepBatchSize)
	}
	return nil
}
