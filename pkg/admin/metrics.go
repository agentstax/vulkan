package admin

import (
	"context"
	"fmt"

	"github.com/agentstax/vulkan/pkg/metrics"
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
		return nil, fmt.Errorf("%w: %s version %d", topic.ErrTopicNotFound, name, version)
	}

	return a.metricsController.TopicSnapshot(ctx, t.Id)
}

// ListSamples returns the current head per (name, attributes)
// series on __system.metrics.
// Returns ErrTopicNotFound until RegisterSystem has run.
func (a *MessageAdmin) ListSamples(ctx context.Context) ([]*producer.MessageRow[metrics.Sample], error) {
	found, err := a.topicController.GetTopic(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("%w: topic %q -- run RegisterSystem first", topic.ErrTopicNotFound, metrics.TopicName)
	}
	return a.sampleHeads.ListCompactionHeads(ctx, found.Id)
}

// ListSampleMessages returns one series' retained samples, newest first.
// compactionKey is metrics.SampleKey(name, attributes); limit is required.
// Returns ErrTopicNotFound until RegisterSystem has run.
func (a *MessageAdmin) ListSampleMessages(ctx context.Context, compactionKey string, limit int) ([]*producer.MessageRow[metrics.Sample], error) {
	found, err := a.topicController.GetTopic(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, fmt.Errorf("%w: topic %q -- run RegisterSystem first", topic.ErrTopicNotFound, metrics.TopicName)
	}
	return a.sampleHeads.ListCompactionKeyMessages(ctx, found.Id, compactionKey, limit)
}
