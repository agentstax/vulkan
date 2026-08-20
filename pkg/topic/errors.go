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
