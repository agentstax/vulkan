package metrics

import (
	"github.com/agentstax/vulkan/pkg/datastore"
)

// TopicMetrics is the topic-level slot in the composed Metrics. It carries no
// otel instruments today -- queue state turned out to be per (group, topic),
// so those gauges live in consumer/metrics; anything keyed by topic alone
// belongs here.
type TopicMetrics struct {
	datastore *TopicMetricsDatastore
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewTopicMetrics(ds *datastore.PostgresDatastore, cfg *TopicMetricsDatastoreConfig) (*TopicMetrics, error) {
	topicMetricsDatastore, err := NewTopicMetricsDatastore(ds, cfg)
	if err != nil {
		return nil, err
	}

	return &TopicMetrics{
		datastore: topicMetricsDatastore,
	}, nil
}
