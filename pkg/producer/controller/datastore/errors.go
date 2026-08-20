package datastore

import "github.com/agentstax/vulkan/pkg/common"

// errPartitionLockTimeout reclassifies a lock_timeout expiry (55P03) on the
// create-ahead path: lock contention is exactly what the run's backoff
// schedule exists to ride out, while the heal path keeps its fail-fast read.
var errPartitionLockTimeout = common.NewError("VK0018", common.Transient,
	"could not create the covering partition", "")
