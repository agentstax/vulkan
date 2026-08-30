package schedule

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventJobRequestAlreadyPublished means a scheduler tick found its request
// already in the topic: an earlier tick's commit confirmation was lost after
// the publish landed, so this tick republishes nothing.
var EventJobRequestAlreadyPublished = diagnostic.NewEvent("VK0037",
	"schedule request was already published by an earlier ambiguous commit", "")
