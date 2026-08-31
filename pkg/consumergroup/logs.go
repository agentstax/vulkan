package consumergroup

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventLeaseReclaimed means a range lease's worker stopped renewing and the
// range went back to the claimable pool.
//
// Diagnose queries: vulkan explain VK0026
var EventLeaseReclaimed = diagnostic.NewEvent("VK0026",
	"lease reclaimed from expired worker", "").
	Diagnose(
		diagnostic.NewQuery("the leases this group holds now", `
SELECT
	token,
	low,
	high,
	expires_at,
	reclaims
FROM claim_lease_{topic_id}
WHERE consumer_group_id = {group_id}
ORDER BY low;`),
		diagnostic.NewQuery("what the reclaimed range left behind", `
SELECT
	message_id,
	status,
	attempts,
	last_error
FROM exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id BETWEEN {low} AND {high}
ORDER BY message_id;`),
	)

// EventRangeQuarantined means a range hit MaxRangeReclaims and is treated as
// poison instead of being handed out again.
//
// Diagnose queries: vulkan explain VK0027
var EventRangeQuarantined = diagnostic.NewEvent("VK0027",
	"range quarantined after max reclaims",
	"messages written as 'ready' exceptions").
	Diagnose(
		diagnostic.NewQuery("the exceptions the quarantine wrote", `
SELECT
	message_id,
	status,
	attempts,
	can_run_after,
	last_error
FROM exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id BETWEEN {low} AND {high}
ORDER BY message_id;`),
		diagnostic.NewQuery("the messages in the range, to find what kills a consumer", `
SELECT
	id,
	routing_key,
	payload
FROM message_log_{topic_id}
WHERE id BETWEEN {low} AND {high}
ORDER BY id;`),
	)

// EventMessagesDeadLettered marks a commit that wrote terminal outcomes for
// a batch of messages.
//
// Diagnose queries: vulkan explain VK0028
var EventMessagesDeadLettered = diagnostic.NewEvent("VK0028",
	"messages dead-lettered",
	"unrecoverable, will not be retried").
	Diagnose(
		diagnostic.NewQuery("every dead row this group holds, newest first", `
SELECT
	message_id,
	attempts,
	last_error,
	updated_at
FROM exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND status = 'dead'
ORDER BY updated_at DESC;`),
		diagnostic.NewQuery("which errors account for them", `
SELECT last_error, count(*) AS dead_count
FROM exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND status = 'dead'
GROUP BY last_error
ORDER BY dead_count DESC;`),
	)

// EventMessageDeadLettered marks one delivery written as terminal.
//
// Diagnose queries: vulkan explain VK0029
var EventMessageDeadLettered = diagnostic.NewEvent("VK0029",
	"message dead-lettered",
	"unrecoverable, will not be retried").
	Diagnose(
		diagnostic.NewQuery("the delivery row the dead-lettering wrote", `
SELECT
	status,
	attempts,
	last_error,
	updated_at
FROM exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id = {message_id};`),
		diagnostic.NewQuery("every attempt it made, oldest first", `
SELECT
	attempt,
	status,
	error,
	attempted_at
FROM delivery_log_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id = {message_id}
ORDER BY attempt;`),
		diagnostic.NewQuery("the message itself", `
SELECT
	id,
	routing_key,
	payload,
	created_at
FROM message_log_{topic_id}
WHERE id = {message_id};`),
	)

// EventExceptionDeadLettered marks one exception written as terminal.
//
// Diagnose queries: vulkan explain VK0030
var EventExceptionDeadLettered = diagnostic.NewEvent("VK0030",
	"exception dead-lettered",
	"unrecoverable, will not be retried").
	Diagnose(
		diagnostic.NewQuery("the exception row now recorded dead", `
SELECT
	status,
	attempts,
	last_error,
	updated_at
FROM exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id = {message_id};`),
		diagnostic.NewQuery("the attempts that exhausted its budget", `
SELECT
	attempt,
	status,
	error,
	attempted_at
FROM delivery_log_{topic_id}
WHERE consumer_group_id = {group_id}
	AND message_id = {message_id}
ORDER BY attempt;`),
	)

// EventKillBackstopFired means the crash-loop backstop marked a group's
// exceptions dead after repeated consumer crashes on the same rows.
//
// Diagnose queries: vulkan explain VK0031
var EventKillBackstopFired = diagnostic.NewEvent("VK0031",
	"crash-loop kill backstop fired",
	"exceptions marked dead").
	Diagnose(
		diagnostic.NewQuery("the rows the backstop marked dead", `
SELECT
	message_id,
	attempts,
	last_error,
	updated_at
FROM exception_queue_{topic_id}
WHERE consumer_group_id = {group_id}
	AND status = 'dead'
ORDER BY updated_at DESC;`),
		diagnostic.NewQuery("the attempts that crashed without recording an outcome", `
SELECT
	message_id,
	attempt,
	status,
	attempted_at
FROM delivery_log_{topic_id}
WHERE consumer_group_id = {group_id}
	AND status = 'expired'
ORDER BY attempted_at DESC
LIMIT 50;`),
	)

// EventStoredOptionsClamped means a stored message's options fell outside
// this consumer's MessageMin/MessageMax bounds.
var EventStoredOptionsClamped = diagnostic.NewEvent("VK0032",
	"stored message options outside this consumer's bounds", "clamped")

// EventSlowDispatch means one delivery's dispatch ran past the group's
// SlowDispatchThreshold, whatever the delivery's outcome.
var EventSlowDispatch = diagnostic.NewEvent("VK0039",
	"delivery dispatch exceeded the duration threshold", "")

// EventGroupConfigNotRefreshed means an instance could not read its worker
// row's stored config back, so it keeps running on the copy it already has.
//
// Diagnose queries: vulkan explain VK0060
var EventGroupConfigNotRefreshed = diagnostic.NewEvent("VK0060",
	"could not refresh group config",
	"the last copy stays in use").
	Diagnose(
		diagnostic.NewQuery("the config document stored on this group's worker rows", `
SELECT worker_config.name, worker_config.metadata
FROM worker_config
JOIN consumer_group_config ON consumer_group_config.id = worker_config.consumer_group_id
WHERE consumer_group_config.name = '{group}'
ORDER BY worker_config.name;`),
	)
