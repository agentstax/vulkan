package vulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/alert"
)

// AlertHandle is one alert message key plus the client, holding no row.
type AlertHandle struct {
	messageKey string
	client     *Client
}

// Alerts returns every current alert, ordered by message key.
func (s *SystemHandle) Alerts(ctx context.Context) ([]*Message[Alert], error) {
	return s.client.admin.ListAlerts(ctx)
}

// Alert names an alert by its message key. No I/O and no failure -- each verb
// on the handle resolves the key when called.
func (s *SystemHandle) Alert(messageKey string) *AlertHandle {
	return &AlertHandle{messageKey: messageKey, client: s.client}
}

func (a *AlertHandle) MessageKey() string {
	return a.messageKey
}

// Get returns the alert's current value, or nil if no retained message has the
// key.
func (a *AlertHandle) Get(ctx context.Context) (*Message[Alert], error) {
	return a.client.Topic(alert.TopicName).CompactionHead[Alert](ctx, a.messageKey)
}

// Messages returns the alert's retained values, newest first.
func (a *AlertHandle) Messages(ctx context.Context, limit int) ([]*Message[Alert], error) {
	return a.client.Topic(alert.TopicName).KeyMessages[Alert](ctx, a.messageKey, limit)
}
