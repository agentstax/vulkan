package system

import "errors"

// ErrSystemConfigMismatch means RegisterSystem was called with a Config that
// differs from the already-seeded system row.
var ErrSystemConfigMismatch = errors.New("system config does not match existing system")
