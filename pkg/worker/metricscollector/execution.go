package metricscollector

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/metrics"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"golang.org/x/sync/errgroup"
)

// reads the deployment's snapshots at the row's poll_rate while a heartbeat
// holds the claim, producing one Sample per metric to __system.metrics
type MetricsCollectorExecution struct {
	Owner  *common.Owner
	Config *MetricsCollectorConfig
	Logger logger.Logger

	runner           *controller.InstanceTickRunner
	metrics          *metricscontroller.MetricsController
	topics           *topiccontroller.TopicController
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
		topics:           collector.topics,
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
	if err := i.collectWorkers(ctx); err != nil {
		return err
	}
	if err := i.collectCronJobs(ctx); err != nil {
		return err
	}
	return i.collectTopics(ctx)
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
	sample, err := metrics.NewSample(metrics.SampleUnclaimedWorkers, metrics.KindGauge, float64(unclaimed), metrics.UnitCount("worker"), nil, at)
	if err != nil {
		return err
	}
	if err := i.produceSample(ctx, sample); err != nil {
		return err
	}
	sample, err = metrics.NewSample(metrics.SampleOldestUnclaimedAge, metrics.KindGauge, float64(oldest.Milliseconds()), metrics.UnitMilliseconds, nil, at)
	if err != nil {
		return err
	}
	if err := i.produceSample(ctx, sample); err != nil {
		return err
	}
	sample, err = metrics.NewSample(metrics.SampleFailingWorkers, metrics.KindGauge, float64(failing), metrics.UnitCount("worker"), nil, at)
	if err != nil {
		return err
	}
	return i.produceSample(ctx, sample)
}

func (i *MetricsCollectorExecution) collectCronJobs(ctx context.Context) error {
	jobs, err := i.metrics.CronJobSnapshots(ctx)
	if err != nil {
		return err
	}

	var overdue, suspended int64
	var oldest time.Duration
	for _, job := range jobs {
		if job.Suspended {
			suspended++
			continue
		}
		if job.Overdue {
			overdue++
		}
		if job.DueFor > oldest {
			oldest = job.DueFor
		}
	}

	at := time.Now()
	sample, err := metrics.NewSample(metrics.SampleOverdueJobs, metrics.KindGauge, float64(overdue), metrics.UnitCount("job"), nil, at)
	if err != nil {
		return err
	}
	if err := i.produceSample(ctx, sample); err != nil {
		return err
	}
	sample, err = metrics.NewSample(metrics.SampleOldestDueAge, metrics.KindGauge, float64(oldest.Milliseconds()), metrics.UnitMilliseconds, nil, at)
	if err != nil {
		return err
	}
	if err := i.produceSample(ctx, sample); err != nil {
		return err
	}
	sample, err = metrics.NewSample(metrics.SampleSuspendedJobs, metrics.KindGauge, float64(suspended), metrics.UnitCount("job"), nil, at)
	if err != nil {
		return err
	}
	return i.produceSample(ctx, sample)
}

func (i *MetricsCollectorExecution) collectTopics(ctx context.Context) error {
	topics, err := i.topics.ListTopics(ctx)
	if err != nil {
		return err
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(i.Config.TopicConcurrency)

	for _, current := range topics {
		// samples about __system.metrics would land on the topic they
		// measure, so its own numbers would never settle -- skipped
		if current.Name == metrics.TopicName {
			continue
		}
		group.Go(func() error {
			return i.collectTopic(groupCtx, current)
		})
	}
	return group.Wait()
}

func (i *MetricsCollectorExecution) collectTopic(ctx context.Context, current *topic.Topic) error {
	snapshot, err := i.metrics.TopicSnapshot(ctx, current.Id)
	if err != nil {
		return err
	}

	version := strconv.FormatInt(int64(current.SchemaVersion), 10)
	at := time.Now()

	compacted := float64(0)
	if snapshot.Compacted {
		compacted = 1
	}
	sample, err := metrics.NewSample(metrics.SampleTopicCompacted, metrics.KindGauge, compacted, "", map[string]string{
		"topic":   current.Name,
		"version": version,
	}, at)
	if err != nil {
		return err
	}
	if err := i.produceSample(ctx, sample); err != nil {
		return err
	}

	for _, group := range snapshot.Groups {
		attributes := map[string]string{
			"group":   group.ConsumerGroup,
			"topic":   current.Name,
			"version": version,
		}
		if err := i.collectConsumerGroup(ctx, &group, attributes, at); err != nil {
			return err
		}
	}
	return nil
}

func (i *MetricsCollectorExecution) collectConsumerGroup(ctx context.Context, snapshot *metrics.ConsumerGroupSnapshot, attributes map[string]string, at time.Time) error {
	points := []struct {
		name  string
		value float64
		unit  metrics.Unit
	}{
		{metrics.SampleCursorHead, float64(snapshot.Cursor.Head), metrics.UnitCount("message")},
		{metrics.SampleCursorClaimed, float64(snapshot.Cursor.Claimed), metrics.UnitCount("message")},
		{metrics.SampleCursorCommitted, float64(snapshot.Cursor.Committed), metrics.UnitCount("message")},
		{metrics.SampleCursorBacklog, float64(snapshot.Cursor.Backlog), metrics.UnitCount("message")},
		{metrics.SampleCursorInflight, float64(snapshot.Cursor.Inflight), metrics.UnitCount("message")},
		{metrics.SampleReadyExceptions, float64(snapshot.Exceptions.Ready), metrics.UnitCount("exception")},
		{metrics.SampleInflightExceptions, float64(snapshot.Exceptions.Inflight), metrics.UnitCount("exception")},
		{metrics.SampleDeferredExceptions, float64(snapshot.Exceptions.Deferred), metrics.UnitCount("exception")},
		{metrics.SampleDeadExceptions, float64(snapshot.Exceptions.Dead), metrics.UnitCount("exception")},
		{metrics.SampleOldestUnresolvedAge, float64(snapshot.Exceptions.OldestUnresolvedAge.Milliseconds()), metrics.UnitMilliseconds},
		{metrics.SampleOpenLeases, float64(snapshot.OpenLeases), metrics.UnitCount("lease")},
		{metrics.SampleAbandonedOutstanding, float64(snapshot.AbandonedRoutines.Outstanding), metrics.UnitCount("routine")},
		{metrics.SampleAbandonedTotal, float64(snapshot.AbandonedRoutines.Total), metrics.UnitCount("routine")},
		{metrics.SampleAbandonedSelfClearLatencyAvg, float64(snapshot.AbandonedRoutines.SelfClearLatencyAvg.Milliseconds()), metrics.UnitMilliseconds},
	}

	samples := make([]*metrics.Sample, 0, len(points))
	for _, point := range points {
		sample, err := metrics.NewSample(point.name, metrics.KindGauge, point.value, point.unit, attributes, at)
		if err != nil {
			return err
		}
		samples = append(samples, sample)
	}

	// concurrent sends share the producer's batched transactions; serial
	// sends would commit one transaction per sample
	group, groupCtx := errgroup.WithContext(ctx)
	for _, sample := range samples {
		group.Go(func() error {
			return i.produceSample(groupCtx, sample)
		})
	}
	return group.Wait()
}

func (i *MetricsCollectorExecution) produceSample(ctx context.Context, sample *metrics.Sample) error {
	_, err := i.producerInstance.Produce(ctx, sample, producer.ProduceOptions{
		RoutingKey:    sample.Name,
		CompactionKey: metrics.SampleKey(sample.Name, sample.Attributes),
	})
	return err
}
