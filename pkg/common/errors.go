package common

import "github.com/agentstax/vulkan/pkg/common/diagnostic"

// ErrAlreadyConsuming means Consume ran twice at once on one instance -- an
// instance runs one Consume at a time.
var ErrAlreadyConsuming = diagnostic.NewError("VK0001", diagnostic.Permanent,
	"instance is already consuming",
	"wait for the running Consume to return, or Register another instance")

// ErrLifecycleContextNotCancellable means Consume's ctx can never be
// cancelled (e.g. context.Background()), so shutdown could never be
// requested.
var ErrLifecycleContextNotCancellable = diagnostic.NewError("VK0002", diagnostic.Permanent,
	"lifecycle context can never be cancelled",
	"pass the application's shutdown context, or set the config's DisableGracefulShutdown")

// ErrLeaseLost means the row was reclaimed by another consumer between the
// claim and the write; the delivery machinery handles the redelivery.
var ErrLeaseLost = diagnostic.NewError("VK0003", diagnostic.Permanent,
	"lease lost to another consumer", "")

// ErrCommitConfirmationLost means the connection died at Commit with
// outcomes already queued: whether they landed is unconfirmable, so a retry
// could record duplicates -- the lease's expiry sorts the truth out.
var ErrCommitConfirmationLost = diagnostic.NewError("VK0019", diagnostic.Permanent,
	"commit confirmation was lost", "")
