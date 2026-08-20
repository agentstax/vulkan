package common

// ErrAlreadyConsuming means Consume ran twice at once on one instance -- an
// instance runs one Consume at a time.
var ErrAlreadyConsuming = NewError("VK0001", Permanent,
	"instance is already consuming",
	"wait for the running Consume to return, or Register another instance")

// ErrLifecycleContextNotCancellable means Consume's ctx can never be
// cancelled (e.g. context.Background()), so shutdown could never be
// requested.
var ErrLifecycleContextNotCancellable = NewError("VK0002", Permanent,
	"lifecycle context can never be cancelled",
	"pass the application's shutdown context, or set the config's DisableGracefulShutdown")

// ErrLeaseLost means the row was reclaimed by another consumer between the
// claim and the write; the delivery machinery handles the redelivery.
var ErrLeaseLost = NewError("VK0003", Permanent,
	"lease lost to another consumer", "")

// ErrCommitConfirmationLost means the connection died at Commit with
// outcomes already queued: whether they landed is unconfirmable, so a retry
// could record duplicates -- the lease's expiry sorts the truth out.
var ErrCommitConfirmationLost = NewError("VK0019", Permanent,
	"commit confirmation was lost", "")
