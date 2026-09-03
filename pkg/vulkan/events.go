package vulkan

// Every declared log event, under its own name, for callers that filter
// on a code.

import (
	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/produce"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
)

var (
	EventAlertConditionHolds       = alert.EventAlertConditionHolds
	EventConsumerStopped           = consume.EventConsumerStopped
	EventExceptionDeadLettered     = consume.EventExceptionDeadLettered
	EventGroupConfigNotRefreshed   = consume.EventGroupConfigNotRefreshed
	EventKillBackstopFired         = consume.EventKillBackstopFired
	EventLeaseReclaimed            = consume.EventLeaseReclaimed
	EventMessageDeadLettered       = consume.EventMessageDeadLettered
	EventMessagesDeadLettered      = consume.EventMessagesDeadLettered
	EventRangeQuarantined          = consume.EventRangeQuarantined
	EventSlowDispatch              = consume.EventSlowDispatch
	EventStoredOptionsClamped      = consume.EventStoredOptionsClamped
	EventGoRoutineEventsDropped    = metrics.EventGoRoutineEventsDropped
	EventPartitionCreatedOnInsert  = produce.EventPartitionCreatedOnInsert
	EventPartitionNotCreatedAhead  = produce.EventPartitionNotCreatedAhead
	EventSlowProduce               = produce.EventSlowProduce
	EventMessageAlreadyProduced    = schedule.EventMessageAlreadyProduced
	EventScheduleConfigReplaced    = schedule.EventScheduleConfigReplaced
	EventTargetKeepsNoSuccessRows  = schedule.EventTargetKeepsNoSuccessRows
	EventSystemManagerStopped      = system.EventSystemManagerStopped
	EventTopicConfigReplaced       = topic.EventTopicConfigReplaced
	EventInstanceLost              = worker.EventInstanceLost
	EventManagerRowSuspended       = worker.EventManagerRowSuspended
	EventSlowTick                  = worker.EventSlowTick
	EventTickBackoffCurveExhausted = worker.EventTickBackoffCurveExhausted
	EventWorkerConfigReplaced      = worker.EventWorkerConfigReplaced
)
