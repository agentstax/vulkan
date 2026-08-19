package waterline

import (
	"fmt"
	"time"
)

// waterlineMetadata is the config stored on the waterline worker row.
type waterlineMetadata struct {
	PollRate time.Duration `json:"poll_rate"`
}

func (m *waterlineMetadata) Validate() error {
	if m.PollRate <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate)
	}
	return nil
}

// ***************
// *** HELPERS ***
// ***************

// defaultWaterlineMetadata is the config every group's declaration starts with.
func defaultWaterlineMetadata() *waterlineMetadata {
	return &waterlineMetadata{PollRate: time.Second}
}
