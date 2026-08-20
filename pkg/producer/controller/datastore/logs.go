package datastore

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// eventPartitionNotCreatedAhead means the create-ahead pass gave up on the
// next partition; the write path still covers it.
var eventPartitionNotCreatedAhead = diagnostic.NewEvent("VK0033",
	"could not create partition ahead",
	"the first insert past the boundary will create it")
