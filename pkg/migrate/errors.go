package migrate

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrNotRegistered means the queried owner has no baseline record -- the system
// or topic was never registered, or migration_log is missing.
var ErrNotRegistered = diagnostic.NewDiagnosticError("VK0017", diagnostic.RecoveryPermanent,
	"system not registered",
	"register the system with Client.RegisterSystem first")

// ErrSchemaOlderThanBuild means the stored schema version is below what this
// build requires.
//
// Diagnose queries: vulkan explain VK0022
var ErrSchemaOlderThanBuild = diagnostic.NewDiagnosticError("VK0022", diagnostic.RecoveryPermanent,
	"schema version is older than this build requires",
	"migrate the {owner_kind} schema up from {version} to {build_version}").
	Diagnose(
		diagnostic.NewDiagnosticQuery("the steps this database recorded, newest first", `
SELECT
	id,
	migration_version,
	min_compatible_version,
	status,
	created_at
FROM {schema}.migration_log
ORDER BY id DESC
LIMIT 20;`),
	)

// ErrSchemaNewerThanBuild means the database was migrated past this build by
// a step whose MinCompatibleVersion is above it.
//
// Diagnose queries: vulkan explain VK0023
var ErrSchemaNewerThanBuild = diagnostic.NewDiagnosticError("VK0023", diagnostic.RecoveryPermanent,
	"schema version is newer than this build understands",
	"upgrade the binary to one whose build version is at least {version}").
	Diagnose(
		diagnostic.NewDiagnosticQuery("the steps this database recorded, newest first", `
SELECT
	id,
	migration_version,
	min_compatible_version,
	status,
	created_at
FROM {schema}.migration_log
ORDER BY id DESC
LIMIT 20;`),
		diagnostic.NewDiagnosticQuery("which step raised the floor past this build", `
SELECT
	migration_version,
	min_compatible_version,
	created_at
FROM {schema}.migration_log
WHERE min_compatible_version > {build_version}
ORDER BY migration_version;`),
	)

// ErrStepLockTimeout reclassifies a lock_timeout expiry (55P03) on the txn
// step path: lock contention is what the step retry exists to ride out, while
// IsTransientPgError alone would stop the run.
var ErrStepLockTimeout = diagnostic.NewDiagnosticError("VK0053", diagnostic.RecoveryTransient,
	"could not take a lock needed by the migration step",
	"end the blocking session (pg_stat_activity), then run the migration again")
