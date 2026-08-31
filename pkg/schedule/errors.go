package schedule

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// ErrScheduleNotFound means the named schedule has no row.
var ErrScheduleNotFound = diagnostic.NewError("VK0013", diagnostic.Permanent,
	"schedule not found",
	`register "{schedule}" with Client.RegisterSchedule first`)

// ErrDeclarationInterrupted means the schedule row was deleted between the
// declaration's insert attempt and its update; an unchanged retry re-creates
// the row, so DatastoreRetry heals the race.
var ErrDeclarationInterrupted = diagnostic.NewError("VK0025", diagnostic.Transient,
	"could not finish the schedule declaration",
	"rerun the declaration if the schedule should still exist")
