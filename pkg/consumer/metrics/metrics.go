package metrics

import (
	"context"

	"github.com/agentstax/vulkan/pkg/datastore"
	"go.opentelemetry.io/otel/metric"
)

type ConsumerMetrics struct {
	datastore         *consumerMetricsDatastore
	group             string
	topicID           int64
	AbandonedRoutines *AbandonedRoutines
}

func NewConsumerMetrics(meter metric.Meter, group string, topicID int64, topicName string, ds *datastore.PostgresDatastore, cfg *ConsumerMetricsDatastoreConfig) (*ConsumerMetrics, error) {
	abandonedRoutines, err := NewAbandonedRoutines(meter, group, topicName)
	if err != nil {
		return nil, err
	}

	return &ConsumerMetrics{
		datastore:         NewConsumerDatastore(ds, cfg),
		group:             group,
		topicID:           topicID,
		AbandonedRoutines: abandonedRoutines,
	}, nil
}

// QueueState is this consumer's own live (group, topic) queue picture -- see
// QueueState's own doc comment for what each number means and why it has to
// be a live query rather than in-process state.
func (m *ConsumerMetrics) QueueState(ctx context.Context) (*QueueState, error) {
	return m.datastore.QueueState(ctx, m.topicID, m.group)
}
