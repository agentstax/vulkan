package consumergroup

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventLeaseReclaimed means a range lease's worker stopped renewing and the
// range went back to the claimable pool.
var EventLeaseReclaimed = diagnostic.NewEvent("VK0026",
	"lease reclaimed from expired worker", "")

// EventRangeQuarantined means a range hit MaxRangeReclaims and is treated as
// poison instead of being handed out again.
var EventRangeQuarantined = diagnostic.NewEvent("VK0027",
	"range quarantined after max reclaims",
	"messages written as 'ready' exceptions")

// EventMessagesDeadLettered marks a commit that wrote terminal outcomes for
// a batch of messages.
var EventMessagesDeadLettered = diagnostic.NewEvent("VK0028",
	"messages dead-lettered",
	"unrecoverable, will not be retried")

// EventMessageDeadLettered marks one delivery written as terminal.
var EventMessageDeadLettered = diagnostic.NewEvent("VK0029",
	"message dead-lettered",
	"unrecoverable, will not be retried")

// EventExceptionDeadLettered marks one exception written as terminal.
var EventExceptionDeadLettered = diagnostic.NewEvent("VK0030",
	"exception dead-lettered",
	"unrecoverable, will not be retried")

// EventKillBackstopFired means the crash-loop backstop marked a group's
// exceptions dead after repeated consumer crashes on the same rows.
var EventKillBackstopFired = diagnostic.NewEvent("VK0031",
	"crash-loop kill backstop fired",
	"exceptions marked dead")

// EventStoredOptionsClamped means a stored message's options fell outside
// this consumer's MessageMin/MessageMax bounds.
var EventStoredOptionsClamped = diagnostic.NewEvent("VK0032",
	"stored message options outside this consumer's bounds", "clamped")

// EventSlowDispatch means one delivery's dispatch ran past the group's
// SlowDispatchThreshold, whatever the delivery's outcome.
var EventSlowDispatch = diagnostic.NewEvent("VK0039",
	"delivery dispatch exceeded the duration threshold", "")
