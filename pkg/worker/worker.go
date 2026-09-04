package worker

import (
	"fmt"

	"github.com/agentstax/vulkan/pkg/common"
)

// InstanceTarget is how many live instances of one worker row may run at once
// across the deployment
// positive value   => claim up to that many instances
// NoInstanceTarget => any number of instances
// 0                => worker is suspended
type InstanceTarget int

// NoInstanceTarget lifts the claim gate -- any number of instances can run.
const NoInstanceTarget InstanceTarget = -1

func (t InstanceTarget) Suspended() bool {
	return t == 0
}

func (t InstanceTarget) Validate() error {
	if t == NoInstanceTarget || t > 0 {
		return nil
	}
	return fmt.Errorf("must be %d or > 0, got %d", NoInstanceTarget, t)
}

// Worker is one row of the worker_config table.
type Worker struct {
	Id              int64          `json:"id"`
	Name            string         `json:"worker"`
	Owner           *common.Owner  `json:"owner"`
	Metadata        any            `json:"metadata"` // JSONB; each caller owns its shape
	TargetInstances InstanceTarget `json:"target_instances"`
}
