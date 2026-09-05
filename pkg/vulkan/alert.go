package vulkan

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/alert"
)

// AlertHandle is one alert message key plus the client, holding no row.
type AlertHandle struct {
	messageKey string
	client     *Client
}

// Alerts returns every current alert, ordered by message key.
func (s *SystemHandle) Alerts(ctx context.Context) ([]*StoredMessage[Alert], error) {
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
func (a *AlertHandle) Get(ctx context.Context) (*StoredMessage[Alert], error) {
	head, err := a.client.Topic[Alert](alert.TopicName).Key(a.messageKey).CompactionHead(ctx)
	if errors.Is(err, ErrCompactionHeadNotFound) {
		return nil, nil
	}
	return head, err
}

// Messages returns the alert's retained values, newest first.
func (a *AlertHandle) Messages(ctx context.Context, limit int) ([]*StoredMessage[Alert], error) {
	return a.client.Topic[Alert](alert.TopicName).Key(a.messageKey).Messages(ctx, limit)
}
