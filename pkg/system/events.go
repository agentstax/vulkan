package system

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventSystemManagerStopped is a Run life ending on its own -- a spawned
// worker declared itself unrunnable, or the manager row could not be claimed.
// The caller blocked in Run has no error value coming, so this line is where
// an operator learns of it.
var EventSystemManagerStopped = diagnostic.NewDiagnosticEvent("VK0065",
	"system manager stopped",
	"nothing reconciles the deployment's workers until the loop re-claims the row after its backoff")
