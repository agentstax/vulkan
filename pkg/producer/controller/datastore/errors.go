package datastore

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// errPartitionLockTimeout reclassifies a lock_timeout expiry (55P03) on the
// create-ahead path: lock contention is exactly what the run's backoff
// schedule exists to ride out, while the heal path keeps its fail-fast read.
var errPartitionLockTimeout = diagnostic.NewError("VK0018", diagnostic.Transient,
	"could not create the covering partition", "")

// errPartitionMissing reclassifies a partition-routing failure (23514) whose
// partition the heal has just created: the attempt reruns on the retry
// schedule instead of failing the produce, because a batch's rerun can
// straddle into a second missing partition.
var errPartitionMissing = diagnostic.NewError("VK0056", diagnostic.Transient,
	"could not insert the message, no partition covered its id", "")
