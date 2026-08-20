package janitor

import (
	"fmt"
	"time"
)

// janitorMetadata is the config stored on the janitor worker row.
type janitorMetadata struct {
	PollRate       time.Duration `json:"poll_rate"`
	SweepBatchSize int           `json:"sweep_batch_size"` // rows deleted per sweep transaction
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

// ***************
// *** HELPERS ***
// ***************

// defaultJanitorMetadata is the config every topic's declaration starts with.
func defaultJanitorMetadata() *janitorMetadata {
	return &janitorMetadata{
		PollRate:       5 * time.Second,
		SweepBatchSize: 1000,
	}
}
