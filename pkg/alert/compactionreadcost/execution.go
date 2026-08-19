package compactionreadcost

import (
	"context"
	"errors"
	"time"

	"github.com/agentstax/vulkan/pkg/alert"
	"github.com/agentstax/vulkan/pkg/alert/compactionreadcost/controller"
	alertcontroller "github.com/agentstax/vulkan/pkg/alert/controller"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

// CompactionReadCostExecution consumes the alert's job requests while a
// heartbeat holds the claim.
type CompactionReadCostExecution struct {
	Owner  *common.Owner
	Logger common.Logger

	definition     *CompactionReadCostDefinition
	runner         *workercontroller.InstanceRunner
	repeatInterval time.Duration
	alerts         *alertcontroller.AlertController // built per claimed life in consume
	measurements   *producer.ProducerInstance[metrics.Measurement]
}

func newCompactionReadCostExecution(definition *CompactionReadCostDefinition, owner *common.Owner, claimed *worker.WorkerInstance, repeatInterval time.Duration) (*CompactionReadCostExecution, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}

	runner, err := workercontroller.NewInstanceRunner(definition.workers, claimed, &workercontroller.InstanceRunnerConfig{
		InstanceTTL: definition.Config.InstanceTTL,
		Logger:      common.LoggerWith(definition.Logger, "worker", JobName, "group", owner.Name),
	})
	if err != nil {
		return nil, err
	}

	return &CompactionReadCostExecution{
		Owner:          owner,
		Logger:         definition.Logger,
		definition:     definition,
		runner:         runner,
		repeatInterval: repeatInterval,
	}, nil
}

// Run consumes until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (e *CompactionReadCostExecution) Run(ctx context.Context) error {
	return e.runner.Run(ctx, e.consume)
}

// consume is one claimed life: the alert controller is built here so every
// claim applies the claimed row's repeat_interval against the alerts topic's
// live retention.
func (e *CompactionReadCostExecution) consume(ctx context.Context) error {
	registered, err := e.definition.alertProducer.Register(ctx, alert.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	alerts, err := alertcontroller.NewAlertController(ctx, registered, e.definition.alertHeads, e.repeatInterval, &alertcontroller.ControllerConfig{Logger: e.Logger})
	if err != nil {
		return err
	}
	e.alerts = alerts

	measurements, err := e.definition.measurementProducer.Register(ctx, metrics.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	e.measurements = measurements

	instance, err := e.definition.jobRequestConsumer.Register(ctx, JobName, cron.TopicName, topic.SchemaVersion(1), []string{JobName})
	if err != nil {
		return err
	}
	return instance.Consume(ctx, e.evaluateTopics)
}

func (e *CompactionReadCostExecution) evaluateTopics(ctx context.Context, request *cron.JobRequest) error {
	jobData, err := alertcontroller.ToJobData(request.Data)
	if err != nil {
		return err
	}

	topics, err := e.definition.topics.ListTopics(ctx)
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

		found, err := e.definition.controller.Evaluate(ctx, owner, jobData.Threshold)
		if err != nil {
			failed++
			errs = errors.Join(errs, err)
			continue
		}

		outcome, err := e.alerts.Record(ctx, controller.AlertCompactionReadCost, owner, found)
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
	err = e.produceCheckSummary(ctx, evaluated, failed, published, resolved)
	return errors.Join(errs, err)
}

func (e *CompactionReadCostExecution) produceCheckSummary(ctx context.Context, evaluated int64, failed int64, published int64, resolved int64) error {
	attributes := map[string]string{"alert": controller.AlertCompactionReadCost}
	at := time.Now()

	counts := []struct {
		name  string
		value int64
		unit  metrics.Unit
	}{
		{metrics.MetricCheckTopicsEvaluated, evaluated, metrics.UnitCount("topic")},
		{metrics.MetricCheckTopicsFailed, failed, metrics.UnitCount("topic")},
		{metrics.MetricCheckPublishedAlerts, published, metrics.UnitCount("alert")},
		{metrics.MetricCheckResolvedAlerts, resolved, metrics.UnitCount("alert")},
	}

	items := make([]*producer.ProduceItem[metrics.Measurement], 0, len(counts))
	for _, count := range counts {
		measurement, err := metrics.NewMeasurement(count.name, metrics.KindGauge, float64(count.value), count.unit, attributes, at)
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

	_, err := e.measurements.ProduceBatch(ctx, items...)
	return err
}
