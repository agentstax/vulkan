package janitor

import (
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// janitorMetadata is the worker row's own tuning.
type janitorMetadata struct {
	PollRate       controller.MetadataValue[time.Duration] `json:"poll_rate"`
	SweepBatchSize controller.MetadataValue[int]           `json:"sweep_batch_size"` // rows deleted per sweep transaction
}

// defaultJanitorMetadata is the tuning every topic's declaration starts with.
func defaultJanitorMetadata() *janitorMetadata {
	return &janitorMetadata{
		PollRate:       controller.NewMetadataValue(5 * time.Second),
		SweepBatchSize: controller.NewMetadataValue(1000),
	}
}

func (m *janitorMetadata) Validate() error {
	if m.PollRate.Effective() <= 0 {
		return fmt.Errorf("poll_rate must be > 0, got %v", m.PollRate.Effective())
	}
	if m.SweepBatchSize.Effective() <= 0 {
		return fmt.Errorf("sweep_batch_size must be > 0, got %d", m.SweepBatchSize.Effective())
	}
	return nil
}
