package systemmanager

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// eventSystemManagerStopped is a Run life ending on its own -- a spawned
// worker declared itself unrunnable, or the manager row could not be claimed.
// The caller blocked in Run has no error value coming, so this line is where
// an operator learns of it. Unexported: an API package holds no vocabulary.
var eventSystemManagerStopped = diagnostic.NewEvent("VK0065",
	"system manager stopped",
	"nothing reconciles the deployment's workers until the loop re-claims the row after its backoff")
