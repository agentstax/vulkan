package worker

import "github.com/agentstax/vulkan/pkg/common"

// ErrInstanceLost means the instance row expired or was removed mid-work:
// stop -- a replacement may already be running.
var ErrInstanceLost = common.NewError("VK0012", common.Permanent,
	"worker instance row expired or was removed",
	"stop the work; a replacement may already be running")
