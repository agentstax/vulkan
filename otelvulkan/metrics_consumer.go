package otelvulkan

import (
	"context"

	"github.com/agentstax/vulkan/pkg/consumer"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
)

// MetricsConsumer reads the measurement stream on __system.metrics -- every
// publish, Vulkan's own and yours -- for forwarding into your own backend
// without an otel pipeline.
type MetricsConsumer struct {
	consumer *consumer.Consumer
}

// cfg may be nil or a sparse struct -- the underlying consumer defaults and
// validates it.
func NewMetricsConsumer(ds *iDatastore.PostgresDatastore, cfg *consumer.ConsumerConfig) (*MetricsConsumer, error) {
	measurementConsumer, err := consumer.NewConsumer(ds, cfg)
	if err != nil {
		return nil, err
	}
	return &MetricsConsumer{consumer: measurementConsumer}, nil
}

// Register registers consumerGroup on __system.metrics, returning an
// instance ready to Consume. Measurements route under their metric name, so
// names is the group's binding set -- metric names or wildcard patterns;
// nil = every metric.
// Returns ErrTopicNotFound until RegisterSystem has run.
func (c *MetricsConsumer) Register(ctx context.Context, consumerGroup string, names []string) (*consumer.ConsumerInstance[metrics.Measurement], error) {
	return c.consumer.Register[metrics.Measurement](ctx, consumerGroup, metrics.TopicName, names)
}
