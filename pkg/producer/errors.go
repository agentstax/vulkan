package producer

import "errors"

// ErrNotRegistered means a produce call arrived before Register -- the
// producer has no lifecycle to run under yet. Call Register once, with the
// application's lifecycle context, before producing.
var ErrNotRegistered = errors.New("producer not registered")

// ErrShutdownRequested means Register's lifecycle context is cancelled -- the
// producer refuses new work while anything already queued drains and commits.
var ErrShutdownRequested = errors.New("producer shutdown requested")
