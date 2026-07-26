package metrics

import (
	"github.com/agentstax/vulkan/pkg/datastore"
	"go.opentelemetry.io/otel/metric"
)

// ConsumerMetrics is the consumer-side view of one (group, topic): the
// shared DB-truth queue state this group consumes against, plus runtime
// counters that exist only in this process.
type ConsumerMetrics struct {
	QueueState        *ConsumerGroupState
	AbandonedRoutines *AbandonedRoutines
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewConsumerMetrics(meter metric.Meter, group string, topicID int64, topicName string, topicVersion int64, ds *datastore.PostgresDatastore, cfg *ConsumerMetricsDatastoreConfig) (*ConsumerMetrics, error) {
	metricsDatastore, err := NewConsumerMetricsDatastore(ds, cfg)
	if err != nil {
		return nil, err
	}

	queueState, err := NewConsumerGroupState(meter, group, topicID, topicName, topicVersion, metricsDatastore)
	if err != nil {
		return nil, err
	}

	abandonedRoutines, err := NewAbandonedRoutines(meter, group, topicName, topicVersion)
	if err != nil {
		return nil, err
	}

	return &ConsumerMetrics{
		QueueState:        queueState,
		AbandonedRoutines: abandonedRoutines,
	}, nil
}
