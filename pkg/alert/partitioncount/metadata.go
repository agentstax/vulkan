package partitioncount

import (
	"fmt"
	"time"

	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// partitionCountMetadata is the group-level config for this worker.
// The alert's Declare defines the default keys.
// Operators who alter the group define the override keys.
type partitionCountMetadata struct {
	RepeatInterval workercontroller.MetadataValue[time.Duration] `json:"repeat_interval"`
}

func (m *partitionCountMetadata) Validate() error {
	if m.RepeatInterval.Effective() <= 0 {
		return fmt.Errorf("repeat_interval must be > 0, got %v", m.RepeatInterval.Effective())
	}
	return nil
}
