package worker

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventInstanceLost means this instance's worker_instance row was claimed by
// a replacement while it was still running.
var EventInstanceLost = diagnostic.NewEvent("VK0034",
	"worker instance lost",
	"stopping, a replacement may already be running").
	Diagnose(
		diagnostic.NewQuery("the instances holding this worker's rows now", `
SELECT
	worker_instance.id,
	worker_instance.token,
	worker_instance.expires_at,
	worker_instance.attempts
FROM worker_instance
JOIN worker ON worker.id = worker_instance.worker_id
WHERE worker.name = '{worker}'
ORDER BY worker_instance.expires_at DESC;`),
	)

// EventManagerRowSuspended means the manager's own row has target_instances
// 0, so its workers stop being reconciled.
var EventManagerRowSuspended = diagnostic.NewEvent("VK0035",
	"manager row suspended",
	"its chain goes unreconciled until target_instances is restored").
	Diagnose(
		diagnostic.NewQuery("every suspended worker row -- target_instances 0", `
SELECT
	id,
	name,
	system_id,
	topic_id,
	consumer_group_id
FROM worker
WHERE target_instances = 0
ORDER BY name;`),
	)

// EventTickBackoffCurveExhausted means a tick loop's failure streak passed
// its TickRetry cap -- the failure is no longer self-healing.
var EventTickBackoffCurveExhausted = diagnostic.NewEvent("VK0036",
	"worker tick backoff curve exhausted",
	"ticks continue at its cap").
	Diagnose(
		diagnostic.NewQuery("this worker's rows and their failure streaks", `
SELECT
	worker.id,
	worker.target_instances,
	worker_instance.expires_at,
	worker_instance.attempts
FROM worker
LEFT JOIN worker_instance ON worker_instance.worker_id = worker.id
WHERE worker.name = '{worker}'
ORDER BY worker_instance.attempts DESC;`),
	)

// EventSlowTick means one tick ran longer than the row's own poll_rate --
// the worker is behind its own schedule.
var EventSlowTick = diagnostic.NewEvent("VK0040",
	"worker tick exceeded its poll rate",
	"the next tick is late")
