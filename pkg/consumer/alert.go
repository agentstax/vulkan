package consumer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// logAlerts measures the topic against the same conditions the system's
// alert jobs evaluate on a schedule, and logs any that hold. Log-only:
//   - a register path never writes alerts
//   - a failed measure never fails Register
func (c *Consumer[Message]) logAlerts(ctx context.Context, current *topic.Topic) {
	owner, err := common.NewTopicOwner(current.SystemId, current.Id, current.Name)
	if err != nil {
		c.Logger.WarnContext(ctx, "register-time alert pass failed", "topic", current.Name, "error", err)
		return
	}

	for _, evaluator := range c.evaluators {
		found, err := evaluator.Evaluate(ctx, owner, 0)
		if err != nil {
			c.Logger.WarnContext(ctx, "register-time alert pass failed", "topic", current.Name, "error", err)
			continue
		}
		if found == nil {
			continue
		}
		c.Logger.WarnContext(ctx, found.Message,
			"detail", found.Detail, "hint", found.Hint,
			"alert", found.Name, "owner", found.Owner.Name, "severity", found.Severity)
	}
}
