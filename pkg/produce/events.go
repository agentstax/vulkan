package produce

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventPartitionNotCreatedAhead means the create-ahead pass gave up on the
// next partition; the write path still covers it.
var EventPartitionNotCreatedAhead = diagnostic.NewDiagnosticEvent("VK0033",
	"could not create partition ahead",
	"the first insert past the boundary will create it")

// EventPartitionCreatedOnInsert means an insert found no partition for its
// id and created one itself: create-ahead did not run, or a burst
// outran its triggers.
var EventPartitionCreatedOnInsert = diagnostic.NewDiagnosticEvent("VK0057",
	"no partition covers the next message id",
	"the insert creates it and pays the creation latency; run a consumer for the topic's upkeep or raise PartitionSize")

// EventSlowProduce means one produce call ran past the producer's
// SlowProduceThreshold, whatever the call's outcome.
var EventSlowProduce = diagnostic.NewDiagnosticEvent("VK0038",
	"produce exceeded the duration threshold", "")
