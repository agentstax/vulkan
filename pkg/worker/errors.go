package worker

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrInstanceLost means the instance row expired or was removed mid-work:
// stop -- a replacement may already be running.
var ErrInstanceLost = diagnostic.NewError("VK0012", diagnostic.Permanent,
	"worker instance row expired or was removed",
	"stop the work; a replacement may already be running")

// ErrDeclarationInterrupted means the worker row was deleted between the
// declaration's insert attempt and its update; an unchanged retry re-creates
// the row, so DatastoreRetry heals the race.
var ErrDeclarationInterrupted = diagnostic.NewError("VK0024", diagnostic.Transient,
	"could not finish the worker declaration",
	"rerun the declaration if the worker should still exist")
