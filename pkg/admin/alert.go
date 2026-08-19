package admin

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

// ListAlerts returns the current head per (alert, owner) on __system.alerts --
// each key's latest publish, active or resolved, within the topic's retention
// window.
// Returns ErrTopicNotFound until RegisterSystem has run.
func (a *MessageAdmin) ListAlerts(ctx context.Context) ([]*producer.MessageRow[alert.Alert], error) {
	found, err := a.topicController.Get(ctx, alert.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("%w: topic %q -- run RegisterSystem first", topic.ErrTopicNotFound, alert.TopicName)
	}
	return a.alertHeads.ListHeads(ctx, found.Id)
}
