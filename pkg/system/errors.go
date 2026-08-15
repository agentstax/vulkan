package system

import "errors"

// ErrSystemConfigMismatch means RegisterSystem was called with a Config that
// differs from the already-seeded system row.
var ErrSystemConfigMismatch = errors.New("system config does not match existing system")

// ErrSystemLive means DestroySystem was refused because a worker instance is
// still live -- a manager or consumer is running somewhere.
var ErrSystemLive = errors.New("a worker instance is still live -- stop running managers and consumers, or pass Force")

// ErrTopicsRegistered means DestroySystem was refused because non-system
// topics are still registered.
var ErrTopicsRegistered = errors.New("topics are still registered -- destroy them first, or pass Force to destroy them and their messages")
