package schedule

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventMessageAlreadyProduced means a schedule producer tick found its
// message already in the topic: an earlier tick's commit confirmation was
// lost after the produce landed, so this tick produces nothing.
var EventMessageAlreadyProduced = diagnostic.NewDiagnosticEvent("VK0037",
	"schedule message was already produced by an earlier ambiguous commit", "")

// EventTargetKeepsNoSuccessRows means the schedule's target topic keeps
// failure rows only, so ScheduleStatus can never count a success.
var EventTargetKeepsNoSuccessRows = diagnostic.NewDiagnosticEvent("VK0058",
	"schedule target topic keeps no success rows",
	"ScheduleStatus counts no successes for it; set DeliveryLogMode all on the topic to count them")

// EventScheduleConfigReplaced means a declaration overwrote a schedule row's
// differing config -- two declarers disagree about the schedule.
//
// Diagnose queries: vulkan explain VK0062
var EventScheduleConfigReplaced = diagnostic.NewDiagnosticEvent("VK0062",
	"schedule config replaced",
	"the newest declaration wins; if this is unexpected or repeats on every restart, two services declare this schedule with different configs and overwrite each other").
	Diagnose(
		diagnostic.NewDiagnosticQuery("the schedule row as stored now (schedule_config keeps no declaration trail)", `
SELECT
	name,
	expression,
	concurrency,
	timeout_ns,
	suspended
FROM {schema}.schedule_config
WHERE id = {schedule_id};`),
	)
