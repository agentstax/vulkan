package metrics

import (
	"errors"

	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	maintainmetrics "github.com/agentstax/vulkan/pkg/maintain/metrics"
	topicmetrics "github.com/agentstax/vulkan/pkg/topic/metrics"
	"go.opentelemetry.io/otel/metric"
)

// Metrics composes the independent per-domain metrics views a running
// consumer carries -- the provider packages don't know about each other;
// composition happens here.
type Metrics struct {
	*consumermetrics.ConsumerMetrics
	*topicmetrics.TopicMetrics
	*maintainmetrics.MaintenanceMetrics
}

// meter and ds must not be nil -- all three sub-metrics share them.
func NewMetrics(meter metric.Meter, group string, topicID int64, topicName string, topicVersion int64, ds *datastore.PostgresDatastore, log logger.Logger) (*Metrics, error) {
	if meter == nil {
		return nil, errors.New("meter must not be nil")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	consumerMetrics, err := consumermetrics.NewConsumerMetrics(meter, group, topicID, topicName, topicVersion, ds, &consumermetrics.ConsumerMetricsDatastoreConfig{
		Logger: log,
	})
	if err != nil {
		return nil, err
	}

	topicMetrics, err := topicmetrics.NewTopicMetrics(ds, &topicmetrics.TopicMetricsDatastoreConfig{
		Logger: log,
	})
	if err != nil {
		return nil, err
	}

	maintenanceMetrics, err := maintainmetrics.NewMaintenanceMetrics(meter, ds, &maintainmetrics.MaintenanceMetricsDatastoreConfig{
		Logger: log,
	})
	if err != nil {
		return nil, err
	}

	return &Metrics{
		ConsumerMetrics:    consumerMetrics,
		TopicMetrics:       topicMetrics,
		MaintenanceMetrics: maintenanceMetrics,
	}, nil
}
