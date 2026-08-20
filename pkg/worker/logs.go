package worker

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventInstanceLost means this instance's worker_instance row was claimed by
// a replacement while it was still running.
var EventInstanceLost = diagnostic.NewEvent("VK0034",
	"worker instance lost",
	"stopping, a replacement may already be running")

// EventManagerRowSuspended means the manager's own row has target_instances
// 0, so its workers stop being reconciled.
var EventManagerRowSuspended = diagnostic.NewEvent("VK0035",
	"manager row suspended",
	"its chain goes unreconciled until target_instances is restored")

// EventTickBackoffCurveExhausted means a tick loop's failure streak passed
// its TickRetry cap -- the failure is no longer self-healing.
var EventTickBackoffCurveExhausted = diagnostic.NewEvent("VK0036",
	"worker tick backoff curve exhausted",
	"ticks continue at its cap")
