package consumergroup

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrGroupNotFound means the named group has no row on that topic.
//
// Diagnose queries: vulkan explain VK0014
var ErrGroupNotFound = diagnostic.NewError("VK0014", diagnostic.Permanent,
	"consumer group not found",
	"register a consumer with this group name to create it").
	Diagnose(
		diagnostic.NewQuery("every group registered on this topic", `
SELECT
	consumer_group.id,
	consumer_group.name,
	consumer_group.created_at
FROM consumer_group
JOIN topic ON topic.id = consumer_group.topic_id
WHERE topic.name = '{topic}'
ORDER BY consumer_group.name;`),
		diagnostic.NewQuery("the group row behind an id, if that is what the line carried", `
SELECT
	id,
	topic_id,
	name,
	created_at
FROM consumer_group
WHERE id = {group_id};`),
	)

// ErrGroupLive means Destroy was called while a worker instance still runs
// on the group, without a force override.
//
// Diagnose queries: vulkan explain VK0015
var ErrGroupLive = diagnostic.NewError("VK0015", diagnostic.Permanent,
	"consumer group still has a live consumer",
	"stop the group's consumers, or pass DestroyOptions.Force").
	Diagnose(
		diagnostic.NewQuery("the instances still heartbeating on this group", `
SELECT
	worker.name AS worker,
	worker_instance.id,
	worker_instance.expires_at,
	worker_instance.attempts
FROM worker_instance
JOIN worker ON worker.id = worker_instance.worker_id
WHERE worker.consumer_group_id = {group_id}
	AND worker_instance.expires_at > now()
ORDER BY worker_instance.expires_at;`),
	)

// ErrGroupDeliveriesPending means Destroy was called while the group still
// holds delivery rows, without a force override. Deleting them discards:
//   - ready/inflight/deferred rows -> failures promised a retry
//   - dead rows                    -> the dead-letter record
//
// Diagnose queries: vulkan explain VK0016
var ErrGroupDeliveriesPending = diagnostic.NewError("VK0016", diagnostic.Permanent,
	"consumer group still has delivery rows",
	"pass DestroyOptions.Force to delete them").
	Diagnose(
		diagnostic.NewQuery("what the delivery rows would discard, by status", `
SELECT status, count(*) AS row_count
FROM delivery_{topic_id}
WHERE consumer_group_id = {group_id}
GROUP BY status;`),
		diagnostic.NewQuery("the dead ones, whose dead-letter record goes with them", `
SELECT
	message_id,
	attempts,
	last_error,
	updated_at
FROM delivery_{topic_id}
WHERE consumer_group_id = {group_id}
	AND status = 'dead'
ORDER BY message_id;`),
	)
