package datastore

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// eventPartitionNotCreatedAhead means the create-ahead pass gave up on the
// next partition; the write path still covers it.
var eventPartitionNotCreatedAhead = diagnostic.NewEvent("VK0033",
	"could not create partition ahead",
	"the first insert past the boundary will create it")

// eventPartitionCreatedOnInsert means an insert found no partition for its
// id and created one itself: create-ahead did not run, or a burst
// outran its triggers.
var eventPartitionCreatedOnInsert = diagnostic.NewEvent("VK0057",
	"no partition covers the next message id",
	"the insert creates it and pays the creation latency; run a consumer for the topic's upkeep or raise PartitionSize")
