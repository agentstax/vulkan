package metricscollector

import (
	"context"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// Declare creates the system's metrics collector worker row and writes the
// default config onto it -- the newest declaration wins. Registers run it
// every time, so a declaration lost to a crash heals on the next one.
func (c *MetricsCollectorDefinition) Declare(ctx context.Context, owner *common.Owner) error {
	if err := controller.ValidateOwner(owner, common.OwnerSystem, WorkerMetricsCollector); err != nil {
		return err
	}

	return c.workers.RegisterWorker(ctx, WorkerMetricsCollector, owner, &controller.WorkerConfig{
		Metadata: defaultMetricsCollectorMetadata(),
	})
}

// Provision claims one live instance. nil = declined (target_instances
// already filled) -- not an error, try again later.
func (c *MetricsCollectorDefinition) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	parsed, err := controller.ParseMetadata[metricsCollectorMetadata](metadata)
	if err != nil {
		return nil, err
	}
	if err := parsed.Validate(); err != nil {
		return nil, err
	}
	// producer registration before the claim: a failure here leaves no
	// claimed instance behind to block reconciles until its TTL lapses
	producerInstance, err := c.producer.Register(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return nil, err
	}
	claimed, err := controller.RegisterInstance(ctx, c.workers, workerId, owner, common.OwnerSystem, WorkerMetricsCollector, c.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}
	return newMetricsCollectorExecution(c, owner, claimed, parsed, producerInstance)
}
