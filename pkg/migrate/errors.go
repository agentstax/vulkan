package migrate

import "github.com/agentstax/vulkan/pkg/common"

// ErrNotRegistered means the queried owner has no baseline record -- the system
// or topic was never registered, or migration_log is missing.
var ErrNotRegistered = common.NewError("VK0017", common.Permanent,
	"schema not registered",
	"register the system with MessageAdmin.RegisterSystem first")

// ErrSchemaOlderThanBuild means the stored schema version is below what this
// build requires.
var ErrSchemaOlderThanBuild = common.NewError("VK0022", common.Permanent,
	"schema version is older than this build requires",
	"migrate the database up first")

// ErrSchemaNewerThanBuild means the stored schema version is above what this
// build defines -- a newer binary already migrated the database.
var ErrSchemaNewerThanBuild = common.NewError("VK0023", common.Permanent,
	"schema version is newer than this build understands",
	"upgrade the binary")
