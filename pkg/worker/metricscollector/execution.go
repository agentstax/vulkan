package metricscollector

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/metrics"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// reads the deployment's snapshots at the row's poll_rate while a heartbeat
// holds the claim, producing one Sample per metric to __system.metrics
type MetricsCollectorExecution struct {
	Owner  *common.Owner
	Config *MetricsCollectorConfig
	Logger logger.Logger

	runner           *controller.InstanceTickRunner
	metrics          *metricscontroller.MetricsController
	metadata         *metricsCollectorMetadata
	producerInstance *producer.ProducerInstance[metrics.Sample]
}

func newMetricsCollectorExecution(collector *MetricsCollectorDefinition, owner *common.Owner, claimed *worker.WorkerInstance, metadata *metricsCollectorMetadata, producerInstance *producer.ProducerInstance[metrics.Sample]) (*MetricsCollectorExecution, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}
	if producerInstance == nil {
		return nil, errors.New("producerInstance must not be nil")
	}

	runner, err := controller.NewInstanceTickRunner(collector.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    collector.Config.InstanceTTL,
		JitterFraction: collector.Config.JitterFraction,
		Logger:         logger.With(collector.Logger, "worker", WorkerMetricsCollector, "system", owner.SystemId),
		TickRetry:      collector.Config.CollectRetry,
	})
	if err != nil {
		return nil, err
	}

	return &MetricsCollectorExecution{
		Owner:            owner,
		Config:           collector.Config,
		Logger:           collector.Logger,
		runner:           runner,
		metrics:          collector.metrics,
		metadata:         metadata,
		producerInstance: producerInstance,
	}, nil
}

// Run collects until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *MetricsCollectorExecution) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "metrics collector starting", "system", i.Owner.SystemId, "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.collect)
	if err == nil {
		i.Logger.InfoContext(ctx, "metrics collector stopped", "system", i.Owner.SystemId)
	}
	return err
}

// collect is one collection pass. A failed produce fails the whole pass --
// the next tick reproduces every sample, so nothing is salvaged per sample.
func (i *MetricsCollectorExecution) collect(ctx context.Context) error {
	return i.collectWorkers(ctx)
}

func (i *MetricsCollectorExecution) collectWorkers(ctx context.Context) error {
	workers, err := i.metrics.WorkerSnapshots(ctx)
	if err != nil {
		return err
	}

	var unclaimed, failing int64
	var oldest time.Duration
	for _, snapshot := range workers {
		if snapshot.Status == metrics.WorkerUnclaimed {
			unclaimed++
			if snapshot.UnclaimedFor > oldest {
				oldest = snapshot.UnclaimedFor
			}
		}
		if snapshot.Attempts > 0 {
			failing++
		}
	}

	at := time.Now()
	if err := i.produceSample(ctx, metrics.SampleUnclaimedWorkers, float64(unclaimed), metrics.UnitCount("worker"), at); err != nil {
		return err
	}
	if err := i.produceSample(ctx, metrics.SampleOldestUnclaimedAge, float64(oldest.Milliseconds()), metrics.UnitMilliseconds, at); err != nil {
		return err
	}
	return i.produceSample(ctx, metrics.SampleFailingWorkers, float64(failing), metrics.UnitCount("worker"), at)
}

func (i *MetricsCollectorExecution) produceSample(ctx context.Context, name string, value float64, unit metrics.Unit, at time.Time) error {
	sample, err := metrics.NewSample(name, metrics.KindGauge, value, unit, nil, at)
	if err != nil {
		return err
	}
	_, err = i.producerInstance.Produce(ctx, sample, producer.ProduceOptions{
		RoutingKey:    name,
		CompactionKey: metrics.SampleKey(name, nil),
	})
	return err
}
