package compactionreadcost

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/produce"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/schedule"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// CompactionReadCostInstance consumes the alert's schedule messages while a
// heartbeat holds the claim.
type CompactionReadCostInstance struct {
	Owner  *common.Owner
	Logger logging.Logger

	provisioner    *CompactionReadCostProvisioner
	runner         *workercontroller.InstanceRunner
	repeatInterval time.Duration
	alerts         *alertcontroller.AlertController // built per claimed life in consume
	measurements   *producer.ProducerInstance[metrics.Measurement]
}

func newCompactionReadCostInstance(provisioner *CompactionReadCostProvisioner, owner *common.Owner, claimed *worker.WorkerInstance, repeatInterval time.Duration) (*CompactionReadCostInstance, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}

	runner, err := workercontroller.NewInstanceRunner(provisioner.workers, claimed, &workercontroller.InstanceRunnerConfig{
		InstanceTTL: provisioner.Config.InstanceTTL,
		Logger:      logging.NewPipelineLogger(provisioner.Logger, &logging.PipelineLoggerConfig{Args: []any{"worker", JobName, "group", owner.Name}}),
	})
	if err != nil {
		return nil, err
	}

	return &CompactionReadCostInstance{
		Owner:          owner,
		Logger:         provisioner.Logger,
		provisioner:    provisioner,
		runner:         runner,
		repeatInterval: repeatInterval,
	}, nil
}

// Run consumes until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *CompactionReadCostInstance) Run(ctx context.Context) error {
	return i.runner.Run(ctx, i.consume)
}

// consume is one claimed life: the alert controller is built here so every
// claim applies the claimed row's repeat_interval against the alerts topic's
// live retention.
func (i *CompactionReadCostInstance) consume(ctx context.Context) error {
	registered, err := i.provisioner.producer.Register[alert.Alert](ctx, alert.TopicName, &producer.ProducerConfig{Logger: i.Logger, Retry: i.provisioner.Config.Retry})
	if err != nil {
		return err
	}
	alerts, err := alertcontroller.NewAlertController(ctx, registered, i.provisioner.alertHeads, i.repeatInterval, &alertcontroller.ControllerConfig{Logger: i.Logger})
	if err != nil {
		return err
	}
	i.alerts = alerts

	measurements, err := i.provisioner.producer.Register[metrics.Measurement](ctx, metrics.TopicName, &producer.ProducerConfig{Logger: i.Logger, Retry: i.provisioner.Config.Retry})
	if err != nil {
		return err
	}
	i.measurements = measurements

	instance, err := i.provisioner.scheduleConsumer.Register[alert.JobPayload](ctx, JobName, schedule.TopicName, &consumer.ConsumerConfig{
		Bindings: []string{JobName},
		Logger:   i.Logger,
		Retry:    i.provisioner.Config.Retry,
	})
	if err != nil {
		return err
	}
	return instance.Consume(ctx, i.evaluateTopics, nil)
}

func (i *CompactionReadCostInstance) evaluateTopics(ctx context.Context, jobPayload *alert.JobPayload) error {

	topics, err := i.provisioner.topics.List(ctx)
	if err != nil {
		return err
	}

	// one topic's failure never skips the others
	var evaluated, failed, published, resolved int64
	var errs error
	for _, listed := range topics {
		evaluated++
		owner, err := common.NewTopicOwner(listed.SystemId, listed.Id, listed.Name)
		if err != nil {
			failed++
			errs = errors.Join(errs, err)
			continue
		}

		found, err := i.provisioner.controller.Evaluate(ctx, owner, jobPayload.Threshold)
		if err != nil {
			failed++
			errs = errors.Join(errs, err)
			continue
		}

		outcome, err := i.alerts.Record(ctx, controller.AlertCompactionReadCost, owner, found)
		if err != nil {
			failed++
			errs = errors.Join(errs, err)
			continue
		}
		switch outcome {
		case alert.RecordOutcomeActive:
			published++
		case alert.RecordOutcomeResolved:
			resolved++
		}
	}

	// the summary goes out even on a failed run
	err = i.produceCheckSummary(ctx, evaluated, failed, published, resolved)
	return errors.Join(errs, err)
}

func (i *CompactionReadCostInstance) produceCheckSummary(ctx context.Context, evaluated int64, failed int64, published int64, resolved int64) error {
	attributes := map[string]string{"alert": controller.AlertCompactionReadCost}
	at := time.Now()

	counts := []struct {
		name  string
		value int64
		unit  metrics.MetricUnit
	}{
		{metrics.MetricCheckTopicsEvaluated.Name, evaluated, metrics.MetricUnitCount("topic")},
		{metrics.MetricCheckTopicsFailed.Name, failed, metrics.MetricUnitCount("topic")},
		{metrics.MetricCheckPublishedAlerts.Name, published, metrics.MetricUnitCount("alert")},
		{metrics.MetricCheckResolvedAlerts.Name, resolved, metrics.MetricUnitCount("alert")},
	}

	items := make([]*producer.ProduceItem[metrics.Measurement], 0, len(counts))
	for _, count := range counts {
		measurement, err := metrics.NewMeasurement(count.name, metrics.MetricKindGauge, float64(count.value), count.unit, attributes, at)
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

	_, err := i.measurements.ProduceBatch(ctx, items...)
	return err
}
