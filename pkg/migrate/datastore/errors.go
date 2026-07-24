package datastore

import "errors"

// ErrNotRegistered means the queried entity has no baseline record -- the system
// or topic was never registered, or schema_log is missing.
var ErrNotRegistered = errors.New("schema not registered -- call Register first")
