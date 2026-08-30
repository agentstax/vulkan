package schedule

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventMessageAlreadyProduced means a schedule producer tick found its
// message already in the topic: an earlier tick's commit confirmation was
// lost after the produce landed, so this tick produces nothing.
var EventMessageAlreadyProduced = diagnostic.NewEvent("VK0037",
	"schedule message was already produced by an earlier ambiguous commit", "")

// EventTargetKeepsNoSuccessRows means the schedule's target topic keeps
// failure rows only, so ScheduleStatus can never count a success.
var EventTargetKeepsNoSuccessRows = diagnostic.NewEvent("VK0058",
	"schedule target topic keeps no success rows",
	"ScheduleStatus counts no successes for it; set DeliveryLogMode all on the topic to count them")
