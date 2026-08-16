package otelvulkan

import (
	"context"
	"errors"
	"fmt"
	"strings"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
)

// MetricsProducer publishes your own measurements to __system.metrics,
// where Metrics, the Exporter's /metrics endpoint, and the vulkan metrics
// CLI read them beside Vulkan's own.
type MetricsProducer struct {
	producer *producer.Producer[metrics.Measurement]
}

// cfg may be nil or a sparse struct -- the underlying producer defaults and
// validates it.
func NewMetricsProducer(ds *coredatastore.PostgresDatastore, cfg *producer.ProducerConfig) (*MetricsProducer, error) {
	measurementProducer, err := producer.NewProducer[metrics.Measurement](ds, cfg)
	if err != nil {
		return nil, err
	}
	return &MetricsProducer{producer: measurementProducer}, nil
}

// Register resolves __system.metrics and returns an instance ready to
// Produce. Callable many times -- each call returns an independent instance.
// Returns ErrTopicNotFound until RegisterSystem has run.
func (p *MetricsProducer) Register(ctx context.Context) (*MetricsProducerInstance, error) {
	instance, err := p.producer.Register(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	return newMetricsProducerInstance(instance)
}

// MetricsProducerInstance produces each measurement under its series' compaction
// key, so the newest publish is the series' current value and the topic's
// retained log is its history.
type MetricsProducerInstance struct {
	instance *producer.ProducerInstance[metrics.Measurement]
}

func newMetricsProducerInstance(instance *producer.ProducerInstance[metrics.Measurement]) (*MetricsProducerInstance, error) {
	if instance == nil {
		return nil, errors.New("instance must not be nil")
	}
	return &MetricsProducerInstance{instance: instance}, nil
}

// Produce publishes one measurement under MeasurementKey(name, attributes). Build the
// measurement with metrics.NewMeasurement.
func (p *MetricsProducerInstance) Produce(ctx context.Context, measurement *metrics.Measurement) (*producer.ProduceResult[metrics.Measurement], error) {
	if measurement == nil {
		return nil, errors.New("measurement must not be nil")
	}
	if strings.HasPrefix(measurement.Name, metrics.MetricNameReservedPrefix) {
		return nil, fmt.Errorf("metric name %q uses the %q prefix, reserved for Vulkan's own metrics", measurement.Name, metrics.MetricNameReservedPrefix)
	}

	return p.instance.Produce(ctx, measurement, producer.ProduceOptions{
		RoutingKey:    measurement.Name,
		CompactionKey: metrics.MeasurementKey(measurement.Name, measurement.Attributes),
	})
}
