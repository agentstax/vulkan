package consumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// logAlerts measures the topic against the same conditions the system's
// alert jobs evaluate on a schedule, and logs any that hold. Log-only:
//   - a register path never writes alerts
//   - a failed measure never fails Register
func (c *Consumer) logAlerts(ctx context.Context, current *topic.Topic) {
	owner, err := common.NewTopicOwner(current.SystemId, current.Id, current.Name)
	if err != nil {
		c.Logger.WarnContext(ctx, "could not run register-time alert pass", "topic", current.Name, "error", err)
		return
	}

	for _, evaluator := range c.evaluators {
		found, err := evaluator.Evaluate(ctx, owner, 0)
		if err != nil {
			c.Logger.WarnContext(ctx, "could not run register-time alert pass", "topic", current.Name, "error", err)
			continue
		}
		if found == nil {
			continue
		}
		c.Logger.WarnContext(ctx, alert.EventAlertConditionHolds.Message,
			"code", alert.EventAlertConditionHolds.Code,
			"alert", found.Name, "alert_message", found.Message,
			"detail", found.Detail, "hint", found.Hint,
			"owner", found.Owner.Name, "severity", found.Severity)
	}
}
