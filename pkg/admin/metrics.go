package admin

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/metrics/monitor"
	"github.com/agentstax/vulkan/pkg/topic"
)

// TopicMetrics runs the monitor one-shot
func (a *MessageAdmin) TopicMetrics(ctx context.Context, name string, version topic.SchemaVersion) (*monitor.TopicSnapshot, error) {
	t, err := a.GetTopic(ctx, name, version)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, fmt.Errorf("%w: %s version %d", topic.ErrTopicNotFound, name, version)
	}

	return a.monitor.TopicSnapshot(ctx, t.Id)
}
