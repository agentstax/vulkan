package metrics

import (
	"github.com/agentstax/vulkan/pkg/datastore"
	"go.opentelemetry.io/otel/metric"
)

type ConsumerMetrics struct {
	datastore         *consumerMetricsDatastore
	AbandonedRoutines *AbandonedRoutines
}

func NewConsumerMetrics(meter metric.Meter, group string, topicName string, ds *datastore.PostgresDatastore, cfg *ConsumerMetricsDatastoreConfig) (*ConsumerMetrics, error) {
	abandonedRoutines, err := NewAbandonedRoutines(meter, group, topicName)
	if err != nil {
		return nil, err
	}

	return &ConsumerMetrics{
		datastore:         NewConsumerDatastore(ds, cfg),
		AbandonedRoutines: abandonedRoutines,
	}, nil
}
