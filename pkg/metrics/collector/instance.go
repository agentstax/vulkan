package collector

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/common"
	compactioncontroller "github.com/agentstax/vulkan/pkg/compaction/controller"
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
// holds the claim, producing one Measurement per metric to __system.metrics
type MetricsCollectorInstance struct {
	Owner  *common.Owner
	Config *MetricsCollectorConfig
	Logger common.Logger

	runner           *controller.InstanceTickRunner
	metrics          *metricscontroller.MetricsController
	topics           *topiccontroller.TopicController
	alertHeads       *compactioncontroller.CompactionController[alert.Alert]
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

	logger := common.LoggerWith(collector.Logger, "worker", WorkerMetricsCollector, "system_id", owner.SystemId)
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
	if err := i.collectCronJobs(ctx); err != nil {
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
	measurement, err := metrics.NewMeasurement(metrics.MetricUnclaimedWorkers, metrics.KindGauge, float64(unclaimed), metrics.UnitCount("worker"), nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricOldestUnclaimedAge, metrics.KindGauge, float64(oldest.Milliseconds()), metrics.UnitMilliseconds, nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricFailingWorkers, metrics.KindGauge, float64(failing), metrics.UnitCount("worker"), nil, at)
	if err != nil {
		return err
	}
	return i.produceMeasurement(ctx, measurement)
}

func (i *MetricsCollectorInstance) collectCronJobs(ctx context.Context) error {
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
	measurement, err := metrics.NewMeasurement(metrics.MetricOverdueJobs, metrics.KindGauge, float64(overdue), metrics.UnitCount("job"), nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricOldestDueAge, metrics.KindGauge, float64(oldest.Milliseconds()), metrics.UnitMilliseconds, nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricSuspendedJobs, metrics.KindGauge, float64(suspended), metrics.UnitCount("job"), nil, at)
	if err != nil {
		return err
	}
	return i.produceMeasurement(ctx, measurement)
}

func (i *MetricsCollectorInstance) collectAlerts(ctx context.Context) error {
	alertsTopic, err := i.topics.Get(ctx, alert.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	heads, err := i.alertHeads.ListHeads(ctx, alertsTopic.Id)
	if err != nil {
		return err
	}

	var active, resolved int64
	for _, head := range heads {
		switch head.Message.Status {
		case alert.StatusActive:
			active++
		case alert.StatusResolved:
			resolved++
		}
	}

	at := time.Now()
	measurement, err := metrics.NewMeasurement(metrics.MetricActiveAlerts, metrics.KindGauge, float64(active), metrics.UnitCount("alert"), nil, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
		return err
	}

	measurement, err = metrics.NewMeasurement(metrics.MetricResolvedAlerts, metrics.KindGauge, float64(resolved), metrics.UnitCount("alert"), nil, at)
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

	version := strconv.FormatInt(int64(current.SchemaVersion), 10)
	at := time.Now()

	compacted := float64(0)
	if snapshot.Compacted {
		compacted = 1
	}
	measurement, err := metrics.NewMeasurement(metrics.MetricTopicCompacted, metrics.KindGauge, compacted, "", map[string]string{
		"topic":   current.Name,
		"version": version,
	}, at)
	if err != nil {
		return err
	}
	if err := i.produceMeasurement(ctx, measurement); err != nil {
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

func (i *MetricsCollectorInstance) collectConsumerGroup(ctx context.Context, snapshot *metrics.ConsumerGroupSnapshot, attributes map[string]string, at time.Time) error {
	points := []struct {
		name  string
		value float64
		unit  metrics.Unit
	}{
		{metrics.MetricCursorHead, float64(snapshot.Cursor.Head), metrics.UnitCount("message")},
		{metrics.MetricCursorClaimed, float64(snapshot.Cursor.Claimed), metrics.UnitCount("message")},
		{metrics.MetricCursorCommitted, float64(snapshot.Cursor.Committed), metrics.UnitCount("message")},
		{metrics.MetricCursorBacklog, float64(snapshot.Cursor.Backlog), metrics.UnitCount("message")},
		{metrics.MetricCursorInflight, float64(snapshot.Cursor.Inflight), metrics.UnitCount("message")},
		{metrics.MetricReadyExceptions, float64(snapshot.Exceptions.Ready), metrics.UnitCount("exception")},
		{metrics.MetricInflightExceptions, float64(snapshot.Exceptions.Inflight), metrics.UnitCount("exception")},
		{metrics.MetricDeferredExceptions, float64(snapshot.Exceptions.Deferred), metrics.UnitCount("exception")},
		{metrics.MetricDeadExceptions, float64(snapshot.Exceptions.Dead), metrics.UnitCount("exception")},
		{metrics.MetricOldestUnresolvedAge, float64(snapshot.Exceptions.OldestUnresolvedAge.Milliseconds()), metrics.UnitMilliseconds},
		{metrics.MetricOpenLeases, float64(snapshot.OpenLeases), metrics.UnitCount("lease")},
		{metrics.MetricAbandonedOutstanding, float64(snapshot.AbandonedRoutines.Outstanding), metrics.UnitCount("routine")},
		{metrics.MetricAbandonedTotal, float64(snapshot.AbandonedRoutines.Total), metrics.UnitCount("routine")},
		{metrics.MetricAbandonedSelfClearLatencyAvg, float64(snapshot.AbandonedRoutines.SelfClearLatencyAvg.Milliseconds()), metrics.UnitMilliseconds},
	}

	items := make([]*producer.ProduceItem[metrics.Measurement], 0, len(points))
	for _, point := range points {
		measurement, err := metrics.NewMeasurement(point.name, metrics.KindGauge, point.value, point.unit, attributes, at)
		if err != nil {
			return err
		}
		compaction, err := producer.NewCompactionOptions(metrics.MeasurementKey(measurement.Name, measurement.Attributes), 0)
		if err != nil {
			return err
		}
		item, err := producer.NewProduceItem(measurement, producer.ProduceOptions{
			RoutingKey: measurement.Name,
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
	compaction, err := producer.NewCompactionOptions(metrics.MeasurementKey(measurement.Name, measurement.Attributes), 0)
	if err != nil {
		return err
	}

	_, err = i.producerInstance.Produce(ctx, measurement, producer.ProduceOptions{
		RoutingKey: measurement.Name,
		Compaction: compaction,
	})
	return err
}
