package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
)

// ListAlerts returns the current head per (alert, owner) on __system.alerts --
// each key's latest publish, active or resolved, within the topic's retention
// window.
// Returns migrate.ErrNotRegistered until RegisterSystem has run.
func (a *MessageAdmin) ListAlerts(ctx context.Context) ([]*producer.MessageRow[alert.Alert], error) {
	found, err := a.topicController.Get(ctx, alert.TopicName)
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, migrate.ErrNotRegistered.With("topic", alert.TopicName)
	}
	return a.alertHeads.ListHeads(ctx, found.Id)
}
