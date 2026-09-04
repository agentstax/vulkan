package schedule

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrScheduleNotFound means the named schedule has no row.
var ErrScheduleNotFound = diagnostic.NewDiagnosticError("VK0013", diagnostic.RecoveryPermanent,
	"schedule not found",
	"register the named schedule with Client.Scheduler(name).Register first")

// ErrScheduleDeclarationInterrupted means the schedule row was deleted between the
// declaration's insert attempt and its update; an unchanged retry re-creates
// the row, so DatastoreRetry heals the race.
var ErrScheduleDeclarationInterrupted = diagnostic.NewDiagnosticError("VK0025", diagnostic.RecoveryTransient,
	"could not finish the schedule declaration",
	"rerun the declaration if the schedule should still exist")
