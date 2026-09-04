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

func NewMetricsConsumer(ds *iDatastore.PostgresDatastore) (*MetricsConsumer, error) {
	measurementConsumer, err := consumer.NewConsumer(ds)
	if err != nil {
		return nil, err
	}
	return &MetricsConsumer{consumer: measurementConsumer}, nil
}

// Register registers consumerGroup on __system.metrics, returning an
// instance ready to Consume. Measurements route under their metric name, so
// cfg.Bindings is the group's binding set -- metric names or wildcard
// patterns; nil = every metric.
// Returns ErrTopicNotFound until RegisterSystem has run.
func (c *MetricsConsumer) Register(ctx context.Context, consumerGroup string, cfg *consumer.ConsumerConfig) (*consumer.ConsumerInstance[metrics.Measurement], error) {
	return c.consumer.Register[metrics.Measurement](ctx, consumerGroup, metrics.TopicName, cfg)
}
