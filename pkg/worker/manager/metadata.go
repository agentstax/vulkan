package manager

import (
	"fmt"
	"time"
)

// managerMetadata is the worker row's own tuning.
type managerMetadata struct {
	// PollRate is the discovery cadence: how often the manager refreshes the
	// worker set, spawns/stops instances to match, and sweeps expired
	// instance rows. Instances pace their own work -- this only bounds how
	// promptly a new or removed worker row is noticed.
	PollRate time.Duration `json:"poll_rate"`
}

// defaultManagerMetadata is the tuning the seed starts with.
func defaultManagerMetadata() *managerMetadata {
	return &managerMetadata{PollRate: time.Second}
}

func (m *managerMetadata) Validate() error {
	if m.PollRate <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate)
	}
	return nil
}
