package admin

import "github.com/agentstax/vulkan/pkg/common"

// ErrDestroyDisabled means DestroyTopic was called without AllowDestroy set
// on the admin's config.
var ErrDestroyDisabled = common.NewError("VK0008", common.Permanent,
	"destroy is disabled",
	"set MessageAdminConfig.AllowDestroy")

// ErrReservedTopicName means Register/Rename touched a name under
// SystemTopicPrefix -- reserved for admin's own system topics.
var ErrReservedTopicName = common.NewError("VK0009", common.Permanent,
	"topic name uses the reserved __system. prefix",
	"choose a name outside the __system. prefix")
