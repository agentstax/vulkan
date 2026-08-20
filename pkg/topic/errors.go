package topic

import "github.com/agentstax/vulkan/pkg/common"

// ErrTopicConfigMismatch means Register was called with a PartitionSize the topic wasn't created with.
// Every other config field can be changed by registering again.
var ErrTopicConfigMismatch = common.NewError("VK0004", common.Permanent,
	"topic partition size does not match the existing topic",
	"register with the existing PartitionSize, or use a new topic name")

// ErrTopicNotFound means the named topic has no row.
var ErrTopicNotFound = common.NewError("VK0005", common.Permanent,
	"topic not found",
	"register it with MessageAdmin.RegisterTopic first")

// ErrTopicNotEmpty means Destroy was called on a topic that still holds
// messages, without an explicit force override.
var ErrTopicNotEmpty = common.NewError("VK0006", common.Permanent,
	"topic still holds messages",
	"pass DestroyOptions.Force to destroy them with the topic")

// ErrTopicNameTaken means Rename's target name already belongs to another topic.
var ErrTopicNameTaken = common.NewError("VK0007", common.Permanent,
	"topic name already taken",
	"choose a different name")

// ErrTopicPartitionsRemain means Destroy kept finding new partitions after
// its drop-pass limit -- a producer is likely still writing.
var ErrTopicPartitionsRemain = common.NewError("VK0020", common.Permanent,
	"topic partitions remain after draining",
	"stop the topic's producers and call DestroyTopic again")

// ErrTopicDeclarationInterrupted means the topic row was destroyed between
// the declaration's config write and its re-read; an unchanged retry
// registers the topic fresh, so DatastoreRetry heals the race.
var ErrTopicDeclarationInterrupted = common.NewError("VK0021", common.Transient,
	"could not finish the topic declaration",
	"run RegisterTopic again if the topic should still exist")

// ErrDestroyDisabled means a Destroy* call ran without AllowDestroy set
// on the admin's config.
var ErrDestroyDisabled = common.NewError("VK0008", common.Permanent,
	"destroy is disabled",
	"set MessageAdminConfig.AllowDestroy")

// ErrReservedTopicName means Register/Rename touched a name under
// SystemTopicPrefix -- reserved for the admin's own system topics.
var ErrReservedTopicName = common.NewError("VK0009", common.Permanent,
	"topic name uses the reserved __system. prefix",
	"choose a name outside the __system. prefix")
