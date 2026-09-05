package collector

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
	"github.com/agentstax/vulkan/pkg/metrics"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/produce"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"golang.org/x/sync/errgroup"
)

// reads the deployment's snapshots at the row's poll_rate while a heartbeat
// holds the claim, producing one Measurement per metric to __system.metrics
type MetricsCollectorInstance struct {
	Owner  *common.Owner
	Config *MetricsCollectorConfig
	Logger logging.Logger

	runner           *controller.InstanceTickRunner
	metrics          *metricscontroller.MetricsController
	topics           *topiccontroller.TopicController
	alertHeads       *compactioncontroller.CompactionController
	metadata         *metricsCollectorMetadata
	producerInstance *producer.ProducerInstance[metrics.Measurement]
}

func newMetricsCollectorInstance(collector *MetricsCollectorProvisioner, owner *common.Owner, claimed *worker.WorkerInstance, metadata *metricsCollectorMetadata, producerInstance *producer.ProducerInstance[metrics.Measurement]) (*MetricsCollectorInstance, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}
	if producerInstance == nil {
		return nil, errors.New("producerInstance must not be nil")
	}

	logger := logging.NewPipelineLogger(collector.Logger, &logging.PipelineLoggerConfig{Args: []any{"worker", WorkerMetricsCollector, "system_id", owner.SystemId}})
	runner, err := controller.NewInstanceTickRunner(collector.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    collector.Config.InstanceTTL,
		JitterFraction: collector.Config.JitterFraction,
		Logger:         logger,
		TickRetry:      collector.Config.CollectRetry,
	})
	if err != nil {
		return nil, err
	}

	return &MetricsCollectorInstance{
		Owner:            owner,
		Config:           collector.Config,
		Logger:           logger,
		runner:           runner,
		metrics:          collector.metrics,
		topics:           collector.topics,
		alertHeads:       collector.alertHeads,
		metadata:         metadata,
		producerInstance: producerInstance,
	}, nil
}

// Run collects until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *MetricsCollectorInstance) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "metrics collector starting", "vulkan_version", common.BuildVersion(), "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.collect)
	if err == nil {
		i.Logger.InfoContext(ctx, "metrics collector stopped")
	}
	return err
}

// collect is one collection pass. A failed produce fails the whole pass --
// the next tick reproduces every measurement, so nothing is salvaged per measurement.
func (i *MetricsCollectorInstance) collect(ctx context.Context) error {
	if err := i.collectWorkers(ctx); err != nil {
		return err
	}
	if err := i.collectSchedules(ctx); err != nil {
		return err
	}
	if err := i.collectAlerts(ctx); err != nil {
		return err
	}
	return i.collectTopics(ctx)
}

func (i *MetricsCollectorInstance) collectWorkers(ctx context.Context) error {
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
	measurement, err := metrics.NewMeasurement(metrics.MetricUnclaimedWorkers.Name, metrics.MetricKindGauge, float64(unclaimed), metrics.MetricUnitCount("worker"), nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricOldestUnclaimedAge.Name, metrics.MetricKindGauge, float64(oldest.Milliseconds()), metrics.MetricUnitMilliseconds, nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricFailingWorkers.Name, metrics.MetricKindGauge, float64(failing), metrics.MetricUnitCount("worker"), nil, at)
	if err != nil {
		return err
	}
	return i.produceMeasurement(ctx, measurement)
}

func (i *MetricsCollectorInstance) collectSchedules(ctx context.Context) error {
	schedules, err := i.metrics.ScheduleSnapshots(ctx)
	if err != nil {
		return err
	}

	var overdue, suspended int64
	var oldest time.Duration
	for _, found := range schedules {
		if found.Suspended {
			suspended++
			continue
		}
		if found.Overdue {
			overdue++
		}
		if found.DueFor > oldest {
			oldest = found.DueFor
		}
	}

	at := time.Now()
	measurement, err := metrics.NewMeasurement(metrics.MetricOverdueSchedules.Name, metrics.MetricKindGauge, float64(overdue), metrics.MetricUnitCount("found"), nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricOldestDueAge.Name, metrics.MetricKindGauge, float64(oldest.Milliseconds()), metrics.MetricUnitMilliseconds, nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricSuspendedSchedules.Name, metrics.MetricKindGauge, float64(suspended), metrics.MetricUnitCount("found"), nil, at)
	if err != nil {
		return err
	}
	return i.produceMeasurement(ctx, measurement)
}

func (i *MetricsCollectorInstance) collectAlerts(ctx context.Context) error {
	alertsTopic, err := i.topics.Get(ctx, alert.TopicName)
	if err != nil {
		return err
	}
	heads, err := i.alertHeads.ListHeads[alert.Alert](ctx, alertsTopic.Id)
	if err != nil {
		return err
	}

	var active, resolved int64
	for _, head := range heads {
		switch head.Message.Status {
		case alert.AlertStatusActive:
			active++
		case alert.AlertStatusResolved:
			resolved++
		}
	}

	at := time.Now()
	measurement, err := metrics.NewMeasurement(metrics.MetricActiveAlerts.Name, metrics.MetricKindGauge, float64(active), metrics.MetricUnitCount("alert"), nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricResolvedAlerts.Name, metrics.MetricKindGauge, float64(resolved), metrics.MetricUnitCount("alert"), nil, at)
	if err != nil {
		return err
	}
	return i.produceMeasurement(ctx, measurement)
}

func (i *MetricsCollectorInstance) collectTopics(ctx context.Context) error {
	topics, err := i.topics.List(ctx)
	if err != nil {
		return err
	}

	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(i.Config.TopicConcurrency)

	for _, current := range topics {
		// measurements about __system.metrics would land on the topic they
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

func (i *MetricsCollectorInstance) collectTopic(ctx context.Context, current *topic.Topic) error {
	snapshot, err := i.metrics.TopicSnapshot(ctx, current.Id)
	if err != nil {
		return err
	}

	at := time.Now()

	compacted := float64(0)
	if snapshot.Compacted {
		compacted = 1
	}
	measurement, err := metrics.NewMeasurement(metrics.MetricTopicCompacted.Name, metrics.MetricKindGauge, compacted, "", map[string]string{
		"topic": current.Name,
	}, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	for _, group := range snapshot.Groups {
		attributes := map[string]string{
			"group": group.ConsumerGroup,
			"topic": current.Name,
		}
		if err := i.collectConsumerGroup(ctx, &group, attributes, at); err != nil {
			return err
		}
	}
	return nil
}

func (i *MetricsCollectorInstance) collectConsumerGroup(ctx context.Context, snapshot *metrics.ConsumerGroupSnapshot, attributes map[string]string, at time.Time) error {
	points := []struct {
		name  string
		value float64
		unit  metrics.MetricUnit
	}{
		{metrics.MetricCursorHead.Name, float64(snapshot.Cursor.Head), metrics.MetricUnitCount("message")},
		{metrics.MetricCursorClaimed.Name, float64(snapshot.Cursor.Claimed), metrics.MetricUnitCount("message")},
		{metrics.MetricCursorCommitted.Name, float64(snapshot.Cursor.Committed), metrics.MetricUnitCount("message")},
		{metrics.MetricCursorBacklog.Name, float64(snapshot.Cursor.Backlog), metrics.MetricUnitCount("message")},
		{metrics.MetricCursorInflight.Name, float64(snapshot.Cursor.Inflight), metrics.MetricUnitCount("message")},
		{metrics.MetricReadyExceptions.Name, float64(snapshot.Exceptions.Ready), metrics.MetricUnitCount("exception")},
		{metrics.MetricInflightExceptions.Name, float64(snapshot.Exceptions.Inflight), metrics.MetricUnitCount("exception")},
		{metrics.MetricDeferredExceptions.Name, float64(snapshot.Exceptions.Deferred), metrics.MetricUnitCount("exception")},
		{metrics.MetricDeadExceptions.Name, float64(snapshot.Exceptions.Dead), metrics.MetricUnitCount("exception")},
		{metrics.MetricOldestUnresolvedAge.Name, float64(snapshot.Exceptions.OldestUnresolvedAge.Milliseconds()), metrics.MetricUnitMilliseconds},
		{metrics.MetricOpenLeases.Name, float64(snapshot.OpenLeases), metrics.MetricUnitCount("lease")},
		{metrics.MetricAbandonedOutstanding.Name, float64(snapshot.AbandonedRoutines.Outstanding), metrics.MetricUnitCount("routine")},
		{metrics.MetricAbandonedTotal.Name, float64(snapshot.AbandonedRoutines.Total), metrics.MetricUnitCount("routine")},
		{metrics.MetricAbandonedSelfClearLatencyAvg.Name, float64(snapshot.AbandonedRoutines.SelfClearLatencyAvg.Milliseconds()), metrics.MetricUnitMilliseconds},
	}

	items := make([]*producer.ProduceItem[metrics.Measurement], 0, len(points))
	for _, point := range points {
		measurement, err := metrics.NewMeasurement(point.name, metrics.MetricKindGauge, point.value, point.unit, attributes, at)
		if err != nil {
			return err
		}
		compaction, err := produce.NewCompactionOptions(0)
		if err != nil {
			return err
		}
		item, err := producer.NewProduceItem(measurement, &produce.ProduceOptions{
			RoutingKey: measurement.Name,
			MessageKey: metrics.MeasurementKey(measurement.Name, measurement.Attributes),
			Compaction: compaction,
		})
		if err != nil {
			return err
		}
		items = append(items, item)
	}

	_, err := i.producerInstance.ProduceBatch(ctx, items...)
	return err
}

func (i *MetricsCollectorInstance) produceMeasurement(ctx context.Context, measurement *metrics.Measurement) error {
	compaction, err := produce.NewCompactionOptions(0)
	if err != nil {
		return err
	}

	_, err = i.producerInstance.Produce(ctx, measurement, &produce.ProduceOptions{
		RoutingKey: measurement.Name,
		MessageKey: metrics.MeasurementKey(measurement.Name, measurement.Attributes),
		Compaction: compaction,
	})
	return err
}
