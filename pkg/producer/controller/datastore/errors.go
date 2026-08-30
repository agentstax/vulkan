package datastore

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// errPartitionLockTimeout reclassifies a lock_timeout expiry (55P03) on the
// create-ahead path: lock contention is exactly what the run's backoff
// schedule exists to ride out, while the heal path keeps its fail-fast read.
var errPartitionLockTimeout = diagnostic.NewError("VK0018", diagnostic.Transient,
	"could not create the covering partition", "")

// errPartitionCreationBehind is the heal loop's exhaustion: every rerun of
// the insert drew ids past the partition the previous heal created.
var errPartitionCreationBehind = diagnostic.NewError("VK0056", diagnostic.Permanent,
	"partition creation cannot keep up with the id sequence",
	"raise PartitionSize, or run a consumer so create-ahead stays ahead of the producers")
