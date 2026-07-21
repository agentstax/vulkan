// Package errors holds the sentinel errors vulkan's producers and consumers
// share, for callers to errors.Is against.
package errors

import "errors"

// ErrNotRegistered means work arrived before Register -- the instance has no
// lifecycle to run under yet. Call Register once, with the application's
// lifetime context, first.
var ErrNotRegistered = errors.New("not registered")

// ErrShutdownRequested means Register's lifecycle context is cancelled -- the
// instance refuses new work while anything already accepted drains.
var ErrShutdownRequested = errors.New("shutdown requested")

// ErrAlreadyRegistered means Register ran twice on one instance. An instance
// registers once, and a wound-down instance stays down -- construct a new one.
var ErrAlreadyRegistered = errors.New("already registered")

// ErrLifecycleContextNotCancellable means Register was given a context that
// can never be cancelled (e.g. context.Background()), so shutdown could never
// be requested. Pass the application's shutdown context, or opt out with the
// config's DisableGracefulShutdown.
var ErrLifecycleContextNotCancellable = errors.New("lifecycle context can never be cancelled")
