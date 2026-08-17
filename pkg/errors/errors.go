// Package errors holds the sentinel errors vulkan's producers and consumers
// share, for callers to errors.Is against.
package errors

import "errors"

// ErrAlreadyConsuming means Consume ran twice at once on one instance -- an
// instance runs one Consume at a time. Wait for the first to return, or
// Register another instance.
var ErrAlreadyConsuming = errors.New("already consuming")

// ErrLifecycleContextNotCancellable means Consume's ctx can never be
// cancelled (e.g. context.Background()), so shutdown could never be
// requested. Pass the application's shutdown context, or opt out with the
// config's DisableGracefulShutdown.
var ErrLifecycleContextNotCancellable = errors.New("lifecycle context can never be cancelled")

// ErrLeaseLost means the row was reclaimed by another consumer between the
// claim and the write.
var ErrLeaseLost = errors.New("lease lost: row reclaimed by another consumer")
