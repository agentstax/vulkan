package metrics

import (
	"github.com/agentstax/vulkan/pkg/datastore"
	"go.opentelemetry.io/otel/metric"
)

type ConsumerMetrics struct {
	QueueState        *QueueState
	AbandonedRoutines *AbandonedRoutines
}

func NewConsumerMetrics(meter metric.Meter, group string, topicID int64, topicName string, ds *datastore.PostgresDatastore, cfg *ConsumerMetricsDatastoreConfig) (*ConsumerMetrics, error) {
	consumerDatastore, err := NewConsumerDatastore(ds, cfg)
	if err != nil {
		return nil, err
	}

	queueState, err := NewQueueState(meter, group, topicID, topicName, consumerDatastore)
	if err != nil {
		return nil, err
	}

	abandonedRoutines, err := NewAbandonedRoutines(meter, group, topicName)
	if err != nil {
		return nil, err
	}

	return &ConsumerMetrics{
		QueueState:        queueState,
		AbandonedRoutines: abandonedRoutines,
	}, nil
}
