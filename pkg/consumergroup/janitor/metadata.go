package janitor

import (
	"fmt"
	"time"
)

// janitorMetadata is the config stored on the consumer group janitor worker
// row.
type janitorMetadata struct {
	PollRate       time.Duration `json:"poll_rate"`
	SweepBatchSize int           `json:"sweep_batch_size"` // rows deleted per sweep
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

// defaultJanitorMetadata is the config the system's declaration starts with.
// Hourly: rows expire on a 7d TTL, so sweep freshness is noise.
func defaultJanitorMetadata() *janitorMetadata {
	return &janitorMetadata{
		PollRate:       time.Hour,
		SweepBatchSize: 1000,
	}
}
