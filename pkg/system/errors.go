package system

import "errors"

// ErrSystemLive means DestroySystem was refused because a worker instance is
// still live -- a manager or consumer is running somewhere.
var ErrSystemLive = errors.New("a worker instance is still live -- stop running managers and consumers, or pass Force")

// ErrTopicsRegistered means DestroySystem was refused because non-system
// topics are still registered.
var ErrTopicsRegistered = errors.New("topics are still registered -- destroy them first, or pass Force to destroy them and their messages")
