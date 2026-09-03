package consume

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
	consumer_group_config.id,
	consumer_group_config.name,
	consumer_group_config.created_at
FROM {schema}.consumer_group_config
JOIN {schema}.topic_config ON topic_config.id = consumer_group_config.topic_id
WHERE topic_config.name = '{topic}'
ORDER BY consumer_group_config.name;`),
		diagnostic.NewQuery("the group row behind an id, if that is what the line carried", `
SELECT
	id,
	topic_id,
	name,
	created_at
FROM {schema}.consumer_group_config
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
	worker_config.name AS worker,
	worker_instance.id,
	worker_instance.expires_at,
	worker_instance.attempts
FROM {schema}.worker_instance
JOIN {schema}.worker_config ON worker_config.id = worker_instance.worker_id
WHERE worker_config.consumer_group_id = {group_id}
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
FROM {schema}.exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
GROUP BY status;`),
		diagnostic.NewQuery("the dead ones, whose dead-letter record goes with them", `
SELECT
	message_id,
	attempts,
	last_error,
	updated_at
FROM {schema}.exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND status = 'dead'
ORDER BY message_id;`),
	)

// ErrDeliveryTerminal is what Terminal returns: the handler declared that no
// retry could succeed, so the delivery dead-letters on this attempt.
var ErrDeliveryTerminal = diagnostic.NewError("VK0055", diagnostic.Permanent,
	"delivery cannot succeed",
	"")

// ErrDeliveryDelayed is what Delay returns: the handler asked for a later
// run, so the delivery waits out the delay and no failure is counted.
var ErrDeliveryDelayed = diagnostic.NewError("VK0054", diagnostic.Transient,
	"could not complete the delivery yet, the handler asked to run it later",
	"")
