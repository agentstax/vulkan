package otelvulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/topic"
)

// MetricsConsumer reads the sample stream on __system.metrics -- every
// publish, Vulkan's own and yours -- for forwarding into your own backend
// without an otel pipeline.
type MetricsConsumer struct {
	consumer *consumer.Consumer[metrics.Sample]
}

// cfg may be nil or a sparse struct -- the underlying consumer defaults and
// validates it.
func NewMetricsConsumer(ds *coredatastore.PostgresDatastore, cfg *consumer.ConsumerConfig) (*MetricsConsumer, error) {
	sampleConsumer, err := consumer.NewConsumer[metrics.Sample](ds, cfg)
	if err != nil {
		return nil, err
	}
	return &MetricsConsumer{consumer: sampleConsumer}, nil
}

// Register registers consumerGroup on __system.metrics, returning an
// instance ready to Consume. Samples route under their name, so names is
// the group's binding set -- sample names or wildcard patterns; nil = every
// sample.
// Returns ErrTopicNotFound until RegisterSystem has run.
func (c *MetricsConsumer) Register(ctx context.Context, consumerGroup string, names []string) (*consumer.ConsumerInstance[metrics.Sample], error) {
	return c.consumer.Register(ctx, consumerGroup, metrics.TopicName, topic.SchemaVersion(1), names)
}
