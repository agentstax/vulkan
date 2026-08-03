package worker

import (
	"github.com/agentstax/vulkan/pkg/common"
)

// Worker is one row of the worker table.
type Worker struct {
	Id              int64
	Name            string
	Owner           common.Owner
	Metadata        any // JSONB; each caller owns its shape
	TargetInstances int
}
