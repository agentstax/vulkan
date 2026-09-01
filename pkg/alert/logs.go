package alert

import (
	"github.com/agentstax/vulkan/pkg/common/diagnostic"
)

// EventAlertConditionHolds means a Register-time pass measured the topic
// against the built-in alert conditions and one of them held. The pass is
// log-only -- Register never fails on it.
var EventAlertConditionHolds = diagnostic.NewEvent("VK0063",
	"alert condition holds",
	"nothing was published; the scheduled check is what publishes and resolves an alert")
