// Package migrations holds the system-scope schema migration steps -- one
// migrate.Migration per version, gathered into an explicit ordered Registry.
package migrations

import "github.com/agentstax/vulkan/pkg/migrate"

// Registry is the ordered list of system-scope steps above the v1 baseline
// (RegisterSystem IS version 1, so steps start at version 2).
//
// Add a step by declaring it in its own migration_<version>.go file
// and appending it here -- the slice order is the truth.
// See migrate.Migration for the authoring rules.
var Registry = []migrate.Migration{}
