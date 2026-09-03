package worker

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrInstanceLost means the instance row expired or was removed mid-work:
// stop -- a replacement may already be running.
var ErrInstanceLost = diagnostic.NewDiagnosticError("VK0012", diagnostic.RecoveryPermanent,
	"worker instance row expired or was removed",
	"stop the work; a replacement may already be running")

// ErrWorkerDeclarationInterrupted means the worker row was deleted between the
// declaration's insert attempt and its update; an unchanged retry re-creates
// the row, so DatastoreRetry heals the race.
var ErrWorkerDeclarationInterrupted = diagnostic.NewDiagnosticError("VK0024", diagnostic.RecoveryTransient,
	"could not finish the worker declaration",
	"rerun the declaration if the worker should still exist")
