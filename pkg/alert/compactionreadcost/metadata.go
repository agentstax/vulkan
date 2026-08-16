package compactionreadcost

import (
	"fmt"
	"time"
)

// compactionReadCostMetadata is the config stored on the alert's worker row.
type compactionReadCostMetadata struct {
	RepeatInterval time.Duration `json:"repeat_interval"`
}

func (m *compactionReadCostMetadata) Validate() error {
	if m.RepeatInterval <= 0 {
		return fmt.Errorf("repeat_interval must be > 0, got %v", m.RepeatInterval)
	}
	return nil
}
