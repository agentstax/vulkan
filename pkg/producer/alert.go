package producer

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
)

// logAlerts measures the topic against the same conditions the system's
// alert jobs evaluate on a schedule, and logs any that hold. Log-only:
//   - a register path never writes alerts
//   - a failed measure never fails Register
func (p *Producer[Message]) logAlerts(ctx context.Context, current *topic.Topic) {
	owner, err := common.NewTopicOwner(current.SystemId, current.Id, current.Name)
	if err != nil {
		p.logger.WarnContext(ctx, "register-time alert pass failed", "topic", current.Name, "error", err)
		return
	}

	for _, evaluator := range p.evaluators {
		found, err := evaluator.Evaluate(ctx, owner, 0)
		if err != nil {
			p.logger.WarnContext(ctx, "register-time alert pass failed", "topic", current.Name, "error", err)
			continue
		}
		if found == nil {
			continue
		}
		p.logger.WarnContext(ctx, found.Message,
			"detail", found.Detail, "hint", found.Hint,
			"alert", found.Name, "owner", found.Owner.Name, "severity", found.Severity)
	}
}
