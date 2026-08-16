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

// MetricsProducer publishes your own metric samples to __system.metrics,
// where Metrics, the Exporter's /metrics endpoint, and the vulkan metrics
// CLI read them beside Vulkan's own.
type MetricsProducer struct {
	producer *producer.Producer[metrics.Sample]
}

// cfg may be nil or a sparse struct -- the underlying producer defaults and
// validates it.
func NewMetricsProducer(ds *coredatastore.PostgresDatastore, cfg *producer.ProducerConfig) (*MetricsProducer, error) {
	sampleProducer, err := producer.NewProducer[metrics.Sample](ds, cfg)
	if err != nil {
		return nil, err
	}
	return &MetricsProducer{producer: sampleProducer}, nil
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

// MetricsProducerInstance produces each sample under its series' compaction
// key, so the newest publish is the series' current value and the topic's
// retained log is its history.
type MetricsProducerInstance struct {
	instance *producer.ProducerInstance[metrics.Sample]
}

func newMetricsProducerInstance(instance *producer.ProducerInstance[metrics.Sample]) (*MetricsProducerInstance, error) {
	if instance == nil {
		return nil, errors.New("instance must not be nil")
	}
	return &MetricsProducerInstance{instance: instance}, nil
}

// Produce publishes one sample under SampleKey(name, attributes). Build the
// sample with metrics.NewSample.
func (p *MetricsProducerInstance) Produce(ctx context.Context, sample *metrics.Sample) (*producer.ProduceResult[metrics.Sample], error) {
	if sample == nil {
		return nil, errors.New("sample must not be nil")
	}
	if strings.HasPrefix(sample.Name, metrics.SampleNameReservedPrefix) {
		return nil, fmt.Errorf("sample name %q uses the %q prefix, reserved for Vulkan's own samples", sample.Name, metrics.SampleNameReservedPrefix)
	}

	return p.instance.Produce(ctx, sample, producer.ProduceOptions{
		RoutingKey:    sample.Name,
		CompactionKey: metrics.SampleKey(sample.Name, sample.Attributes),
	})
}
