package manager

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// managerMetadata is the worker row's own tuning.
type managerMetadata struct {
	// PollRate is the discovery cadence: how often the manager refreshes the
	// worker set, spawns/stops instances to match, and sweeps expired
	// instance rows. Instances pace their own work -- this only bounds how
	// promptly a new or removed worker row is noticed.
	PollRate controller.MetadataValue[time.Duration] `json:"poll_rate"`
}

// defaultManagerMetadata is the tuning the declaration starts with.
func defaultManagerMetadata() *managerMetadata {
	return &managerMetadata{PollRate: controller.NewMetadataValue(time.Second)}
}

func (m *managerMetadata) Validate() error {
	if m.PollRate.Effective() <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate.Effective())
	}
	return nil
}
