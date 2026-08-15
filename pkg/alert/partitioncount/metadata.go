package partitioncount

import (
	"fmt"
	"time"
)

// partitionCountMetadata is the group-level config for this worker,
// written by the alert's Declare.
type partitionCountMetadata struct {
	RepeatInterval time.Duration `json:"repeat_interval"`
}

func (m *partitionCountMetadata) Validate() error {
	if m.RepeatInterval <= 0 {
		return fmt.Errorf("repeat_interval must be > 0, got %v", m.RepeatInterval)
	}
	return nil
}
