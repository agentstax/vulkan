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
	Owner  *common.Owner
	Name   string
	Status WorkerStatus

	TargetInstances int
	LiveInstances   int

	Attempts     int           // largest consecutive-failure streak across live instances
	UnclaimedFor time.Duration // now() - the newest expires_at, while nothing is live; 0 while claimed, and 0 if expired rows were already deleted
}
