package metrics

import (
	"time"

	"github.com/agentstax/vulkan/pkg/common"
)

type WorkerStatus string

const (
	WorkerSuspended WorkerStatus = "suspended" // target_instances = 0
	WorkerClaimed   WorkerStatus = "claimed"   // at least one live instance row
	WorkerUnclaimed WorkerStatus = "unclaimed" // no live instance row and not suspended
)

// WorkerSnapshot is one worker row's claim state - how many instances:
// - should be running it
// - how many are
// - for how long
type WorkerSnapshot struct {
	Owner  *common.Owner `json:"owner"`
	Name   string        `json:"worker"`
	Status WorkerStatus  `json:"status"`

	TargetInstances int `json:"target_instances"`
	LiveInstances   int `json:"live_instances"`

	Attempts     int           `json:"attempts"`      // largest consecutive-failure streak across live instances
	UnclaimedFor time.Duration `json:"unclaimed_for"` // now() - the newest expires_at, while nothing is live; 0 while claimed, and 0 if expired rows were already deleted
}
