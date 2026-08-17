package worker

import (
	"github.com/agentstax/vulkan/pkg/common"
)

// NoInstanceTarget as a row's target_instances lifts the claim gate -- for
// workers where any number of live instances is correct, like the manager.
const NoInstanceTarget = -1

// Worker is one row of the worker table.
type Worker struct {
	Id       int64
	Name     string
	Owner    common.Owner
	Metadata any // JSONB; each caller owns its shape
}
