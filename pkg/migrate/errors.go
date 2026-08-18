package migrate

import "errors"

// ErrNotRegistered means the queried owner has no baseline record -- the system
// or topic was never registered, or migration_log is missing.
var ErrNotRegistered = errors.New("schema not registered -- call Register first")
