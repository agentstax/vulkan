package consumergroup

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrGroupNotFound means the named group has no row on that topic.
var ErrGroupNotFound = diagnostic.NewError("VK0014", diagnostic.Permanent,
	"consumer group not found",
	"register a consumer with this group name to create it")

// ErrGroupLive means Destroy was called while a worker instance still runs
// on the group, without a force override.
var ErrGroupLive = diagnostic.NewError("VK0015", diagnostic.Permanent,
	"consumer group still has a live consumer",
	"stop the group's consumers, or pass DestroyOptions.Force")

// ErrGroupDeliveriesPending means Destroy was called while the group still
// holds delivery rows, without a force override. Deleting them discards:
//   - ready/inflight/deferred rows -> failures promised a retry
//   - dead rows                    -> the dead-letter record
var ErrGroupDeliveriesPending = diagnostic.NewError("VK0016", diagnostic.Permanent,
	"consumer group still has delivery rows",
	"pass DestroyOptions.Force to delete them")
