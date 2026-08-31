package migrate

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrNotRegistered means the queried owner has no baseline record -- the system
// or topic was never registered, or migration_log is missing.
var ErrNotRegistered = diagnostic.NewError("VK0017", diagnostic.Permanent,
	"schema not registered",
	"register the system with Client.RegisterSystem first")

// ErrSchemaOlderThanBuild means the stored schema version is below what this
// build requires.
//
// Diagnose queries: vulkan explain VK0022
var ErrSchemaOlderThanBuild = diagnostic.NewError("VK0022", diagnostic.Permanent,
	"schema version is older than this build requires",
	"migrate the {owner_kind} schema up from {version} to {build_version}").
	Diagnose(
		diagnostic.NewQuery("the steps this database recorded, newest first", `
SELECT
	id,
	migration_version,
	min_compatible_version,
	status,
	created_at
FROM migration_log
ORDER BY id DESC
LIMIT 20;`),
	)

// ErrSchemaNewerThanBuild means the database was migrated past this build by
// a step whose MinCompatibleVersion is above it.
//
// Diagnose queries: vulkan explain VK0023
var ErrSchemaNewerThanBuild = diagnostic.NewError("VK0023", diagnostic.Permanent,
	"schema version is newer than this build understands",
	"upgrade the binary to one whose build version is at least {version}").
	Diagnose(
		diagnostic.NewQuery("the steps this database recorded, newest first", `
SELECT
	id,
	migration_version,
	min_compatible_version,
	status,
	created_at
FROM migration_log
ORDER BY id DESC
LIMIT 20;`),
		diagnostic.NewQuery("which step raised the floor past this build", `
SELECT
	migration_version,
	min_compatible_version,
	created_at
FROM migration_log
WHERE min_compatible_version > {build_version}
ORDER BY migration_version;`),
	)
