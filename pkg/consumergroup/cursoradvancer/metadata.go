package cursoradvancer

import (
	"fmt"
	"time"
)

// cursorAdvancerMetadata is the config stored on the cursor advancer worker row.
type cursorAdvancerMetadata struct {
	PollRate time.Duration `json:"poll_rate"`
}

func (m *cursorAdvancerMetadata) Validate() error {
	if m.PollRate <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate)
	}
	return nil
}

// ***************
// *** HELPERS ***
// ***************

// defaultCursorAdvancerMetadata is the config every group's declaration starts with.
func defaultCursorAdvancerMetadata() *cursorAdvancerMetadata {
	return &cursorAdvancerMetadata{PollRate: time.Second}
}
