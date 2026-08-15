package waterline

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// waterlineMetadata is the worker row's own tuning.
type waterlineMetadata struct {
	PollRate controller.MetadataValue[time.Duration] `json:"poll_rate"`
}

// defaultWaterlineMetadata is the tuning every group's declaration starts with.
func defaultWaterlineMetadata() *waterlineMetadata {
	return &waterlineMetadata{PollRate: controller.NewMetadataValue(time.Second)}
}

func (m *waterlineMetadata) Validate() error {
	if m.PollRate.Effective() <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate.Effective())
	}
	return nil
}
