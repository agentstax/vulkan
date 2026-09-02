package producer

import (
	"context"
	"errors"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/schedule"
	scheduleproducercontroller "github.com/agentstax/vulkan/pkg/schedule/producer/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
)

// scans schedule_config for due rows at the row's poll_rate while a heartbeat
// holds the claim, producing each due row's stored message onto its target
// topic and advancing the row to its next scheduled time
type ScheduleProducerInstance struct {
	Owner  *common.Owner
	Config *ScheduleProducerConfig
	Logger logging.Logger

	runner     *controller.InstanceTickRunner
	ds         *iDatastore.PostgresDatastore
	controller *scheduleproducercontroller.ScheduleProducerController
	metadata   *scheduleProducerMetadata
	producer   *producer.Producer
}

func newScheduleProducerInstance(scheduleProducer *ScheduleProducerProvisioner, owner *common.Owner, claimed *worker.WorkerInstance, metadata *scheduleProducerMetadata) (*ScheduleProducerInstance, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	logger := logging.NewPipelineLogger(scheduleProducer.Logger, &logging.PipelineLoggerConfig{Args: []any{"worker", WorkerScheduleProducer, "system_id", owner.SystemId}})
	runner, err := controller.NewInstanceTickRunner(scheduleProducer.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    scheduleProducer.Config.InstanceTTL,
		JitterFraction: scheduleProducer.Config.JitterFraction,
		Logger:         logger,
		TickRetry:      scheduleProducer.Config.ScanRetry,
	})
	if err != nil {
		return nil, err
	}

	return &ScheduleProducerInstance{
		Owner:      owner,
		Config:     scheduleProducer.Config,
		Logger:     logger,
		runner:     runner,
		ds:         scheduleProducer.ds,
		controller: scheduleProducer.controller,
		metadata:   metadata,
		producer:   scheduleProducer.producer,
	}, nil
}

// Run scans until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *ScheduleProducerInstance) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "schedule producer starting", "vulkan_version", common.BuildVersion(), "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.scan)
	if err == nil {
		i.Logger.InfoContext(ctx, "schedule producer stopped")
	}
	return err
}

// scan is one scheduler pass: an unlocked scan for due rows, then ONE
// transaction per row -- a shared transaction would let one bad row roll back
// every schedule's produce, and would hold ProduceInTx's whole-topic
// consumer-progress lock from the first produce to the end of the pass.
func (i *ScheduleProducerInstance) scan(ctx context.Context) error {
	ids, err := i.controller.ListDue(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := i.produceDue(ctx, id); err != nil {
			if ctx.Err() != nil {
				return err
			}
			i.Logger.WarnContext(ctx, "could not produce schedule message -- siblings proceed", "schedule_id", id, "error", err)
		}
	}
	return nil
}

// produceDue resolves one due row: recheck under lock, produce the stored
// message for the NEWEST due scheduled time onto the row's target topic,
// advance the row. Produce + advance + idempotency claim share the
// transaction, so an ambiguous-commit replay rolls all three back together
// and the schedule.IdempotencyKey dedupe covers exactly that replay.
func (i *ScheduleProducerInstance) produceDue(ctx context.Context, id int64) error {
	return producer.InTransaction(ctx, i.ds, func(ctx context.Context, tx producer.Tx) error {
		row, err := i.controller.ClaimDue(ctx, tx, id)
		if err != nil || row == nil {
			return err
		}

		expression, err := schedule.ParseExpression(row.Expression)
		if err != nil {
			return err
		}

		// produce the NEWEST due scheduled time -- after downtime, staleness
		// is at most one schedule rate; older due scheduled times are dropped,
		// not produced late. The IsZero guard keeps an unsatisfiable schedule
		// from spinning.
		scheduledTime := row.NextScheduledAt
		for next := expression.Next(scheduledTime); !next.IsZero() && !next.After(row.DbNow); next = expression.Next(scheduledTime) {
			scheduledTime = next
		}

		stored, err := schedule.NewStoredMessage(row.Payload, row.SchemaVersion)
		if err != nil {
			return err
		}

		// registered per produce: the target topic is the row's, and a due
		// row is minute-scale rare
		target, err := i.producer.Register[schedule.StoredMessage](ctx, row.TopicName)
		if err != nil {
			return err
		}

		compaction, err := producer.NewCompactionOptions(0)
		if err != nil {
			return err
		}

		produced, err := target.ProduceInTx(ctx, tx, stored, &producer.ProduceOptions{
			RoutingKey:     row.Name,
			MessageKey:     row.Name,
			Compaction:     compaction,
			IdempotencyKey: schedule.IdempotencyKey(scheduledTime, row.Id).String(),
			Message: &common.MessageOptions{
				Concurrency: row.Concurrency,
				Timeout:     row.Timeout,
				ScheduledAt: scheduledTime,
			},
		})
		if err != nil {
			return err
		}
		if produced.Duplicate {
			// an earlier tick's ambiguous commit produced this message, then
			// failed to advance the row
			i.Logger.WarnContext(ctx, schedule.EventMessageAlreadyProduced.Message, "code", schedule.EventMessageAlreadyProduced.Code, "schedule_id", row.Id, "schedule", row.Name, "scheduled_at", scheduledTime)
		}

		// next scheduled time from the DB clock ONLY -- Go/DB skew
		// double-produces tight schedules
		next := expression.Next(row.DbNow)
		if next.IsZero() {
			// schedule went unsatisfiable (tzdata drift): keep the produce,
			// suspend the row -- it has no honest next_scheduled_at
			i.Logger.WarnContext(ctx, "schedule expression has no next scheduled time -- suspending", "schedule_id", row.Id, "schedule", row.Name, "expression", row.Expression)
			return i.controller.Suspend(ctx, tx, row.Id, scheduledTime)
		}
		return i.controller.Advance(ctx, tx, row.Id, next, scheduledTime)
	})
}
