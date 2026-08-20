package admin

import (
	"context"

	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/migrate"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

// TopicMetrics runs the monitor one-shot
func (a *MessageAdmin) TopicMetrics(ctx context.Context, name string, version topic.SchemaVersion) (*metrics.TopicSnapshot, error) {
	t, err := a.GetTopic(ctx, name, version)
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, topic.ErrTopicNotFound.With("topic", name, "version", version)
	}

	return a.metricsController.TopicSnapshot(ctx, t.Id)
}

// ListMeasurements returns the current head per (name, attributes)
// series on __system.metrics.
// Returns migrate.ErrNotRegistered until RegisterSystem has run.
func (a *MessageAdmin) ListMeasurements(ctx context.Context) ([]*producer.MessageRow[metrics.Measurement], error) {
	found, err := a.topicController.Get(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, migrate.ErrNotRegistered.With("topic", metrics.TopicName)
	}
	return a.measurementHeads.ListHeads(ctx, found.Id)
}

// ListMeasurementMessages returns one series' retained measurements, newest first.
// compactionKey is metrics.MeasurementKey(name, attributes); limit is required.
// Returns migrate.ErrNotRegistered until RegisterSystem has run.
func (a *MessageAdmin) ListMeasurementMessages(ctx context.Context, compactionKey string, limit int) ([]*producer.MessageRow[metrics.Measurement], error) {
	found, err := a.topicController.Get(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, migrate.ErrNotRegistered.With("topic", metrics.TopicName)
	}
	return a.measurementHeads.ListKeyMessages(ctx, found.Id, compactionKey, limit)
}
