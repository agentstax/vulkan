package producer

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// eventSlowProduce means one produce call ran past the producer's
// SlowProduceThreshold, whatever the call's outcome.
var eventSlowProduce = diagnostic.NewEvent("VK0038",
	"produce exceeded the duration threshold", "")
