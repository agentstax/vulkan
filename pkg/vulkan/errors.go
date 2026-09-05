package vulkan

// Every declared error, under its own name: the same value, so errors.Is
// holds whether a caller spells the root or this package.

import (
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/compaction"
	"github.com/agentstax/vulkan/pkg/consume"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/produce"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
)

var (
	ErrAlreadyConsuming               = common.ErrAlreadyConsuming
	ErrCommitConfirmationLost         = common.ErrCommitConfirmationLost
	ErrLeaseLost                      = common.ErrLeaseLost
	ErrLifecycleContextNotCancellable = common.ErrLifecycleContextNotCancellable
	ErrCompactionHeadNotFound         = compaction.ErrCompactionHeadNotFound
	ErrDeliveryDelayed                = consume.ErrDeliveryDelayed
	ErrDeliveryTerminal               = consume.ErrDeliveryTerminal
	ErrGroupDeliveriesPending         = consume.ErrGroupDeliveriesPending
	ErrGroupLive                      = consume.ErrGroupLive
	ErrGroupNotFound                  = consume.ErrGroupNotFound
	ErrNotRegistered                  = migrate.ErrNotRegistered
	ErrSchemaNewerThanBuild           = migrate.ErrSchemaNewerThanBuild
	ErrSchemaOlderThanBuild           = migrate.ErrSchemaOlderThanBuild
	ErrStepLockTimeout                = migrate.ErrStepLockTimeout
	ErrPartitionCreationBehind        = produce.ErrPartitionCreationBehind
	ErrPartitionLockTimeout           = produce.ErrPartitionLockTimeout
	ErrScheduleDeclarationInterrupted = schedule.ErrScheduleDeclarationInterrupted
	ErrScheduleNotFound               = schedule.ErrScheduleNotFound
	ErrSchemaNotCreatable             = system.ErrSchemaNotCreatable
	ErrSystemLive                     = system.ErrSystemLive
	ErrTopicsRegistered               = system.ErrTopicsRegistered
	ErrDestroyDisabled                = topic.ErrDestroyDisabled
	ErrReservedTopicName              = topic.ErrReservedTopicName
	ErrTopicConfigMismatch            = topic.ErrTopicConfigMismatch
	ErrTopicDeclarationInterrupted    = topic.ErrTopicDeclarationInterrupted
	ErrTopicNameTaken                 = topic.ErrTopicNameTaken
	ErrTopicNotEmpty                  = topic.ErrTopicNotEmpty
	ErrTopicNotFound                  = topic.ErrTopicNotFound
	ErrTopicPartitionsRemain          = topic.ErrTopicPartitionsRemain
	ErrInstanceLost                   = worker.ErrInstanceLost
	ErrWorkerDeclarationInterrupted   = worker.ErrWorkerDeclarationInterrupted
)
