package worker

import (
	"github.com/agentstax/vulkan/pkg/common"
)

// NoInstanceTarget as a row's target_instances lifts the claim gate -- for
// workers where any number of live instances is correct, like the manager.
const NoInstanceTarget = -1

// WorkerData is one row of the worker_config table.
type WorkerData struct {
	Id       int64         `json:"id"`
	Name     string        `json:"worker"`
	Owner    *common.Owner `json:"owner"`
	Metadata any           `json:"metadata"` // JSONB; each caller owns its shape
}
