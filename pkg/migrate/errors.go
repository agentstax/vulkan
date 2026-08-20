package migrate

import "github.com/agentstax/vulkan/pkg/common"

// ErrNotRegistered means the queried owner has no baseline record -- the system
// or topic was never registered, or migration_log is missing.
var ErrNotRegistered = common.NewError("VK0017", common.Permanent,
	"schema not registered",
	"register the system with MessageAdmin.RegisterSystem first")
