package consumer

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// eventConsumerStopped is the session summary a consumer instance logs on
// every exit. Unexported: an API package holds no vocabulary, so the event
// stays package-local (VK0038 precedent).
var eventConsumerStopped = diagnostic.NewEvent("VK0041",
	"consumer stopped", "")
