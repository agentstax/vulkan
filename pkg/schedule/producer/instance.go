package producer

import (
	"context"
	"errors"
	"strconv"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/schedule"
	scheduleproducercontroller "github.com/agentstax/vulkan/pkg/schedule/producer/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/google/uuid"
)

// scans schedule for due rows at the row's poll_rate while a heartbeat holds
// the claim, producing one JobRequest per due row and advancing each produced
// row to its next scheduled time
type ScheduleProducerInstance struct {
	Owner  *common.Owner
	Config *ScheduleProducerConfig
	Logger logging.Logger

	runner           *controller.InstanceTickRunner
	ds               *iDatastore.PostgresDatastore
	controller       *scheduleproducercontroller.ScheduleProducerController
	metadata         *scheduleProducerMetadata
	producerInstance *producer.ProducerInstance[schedule.JobRequest]
}

func newScheduleProducerInstance(scheduleProducer *ScheduleProducerProvisioner, owner *common.Owner, claimed *worker.WorkerInstance, metadata *scheduleProducerMetadata, producerInstance *producer.ProducerInstance[schedule.JobRequest]) (*ScheduleProducerInstance, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}
	if producerInstance == nil {
		return nil, errors.New("producerInstance must not be nil")
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
		Owner:            owner,
		Config:           scheduleProducer.Config,
		Logger:           logger,
		runner:           runner,
		ds:               scheduleProducer.ds,
		controller:       scheduleProducer.controller,
		metadata:         metadata,
		producerInstance: producerInstance,
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
		if err := i.produceJobRequest(ctx, id); err != nil {
			if ctx.Err() != nil {
				return err
			}
			i.Logger.WarnContext(ctx, "could not produce schedule request -- siblings proceed", "schedule_id", id, "error", err)
		}
	}
	return nil
}

// produceJobRequest resolves one due row: recheck under lock, produce the
// JobRequest for the NEWEST due scheduled time, advance the row. Produce +
// advance + idempotency claim share the transaction, so an ambiguous-commit
// replay rolls all three back together and the schedule.IdempotencyKey dedupe
// covers exactly that replay.
func (i *ScheduleProducerInstance) produceJobRequest(ctx context.Context, id int64) error {
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

		request, err := schedule.NewJobRequest(row.Id, row.Name, scheduledTime, row.Payload, row.Metadata)
		if err != nil {
			return err
		}

		compaction, err := producer.NewCompactionOptions(0)
		if err != nil {
			return err
		}

		passthrough := func(context.Context, producer.Tx, uuid.UUID) (*schedule.JobRequest, error) {
			return request, nil
		}
		produced, err := i.producerInstance.ProduceInTx(ctx, tx, passthrough, producer.ProduceOptions{
			RoutingKey: row.Name,
			// id not name -- a destroyed name's reuse must not share a key
			MessageKey:     strconv.FormatInt(row.Id, 10),
			Compaction:     compaction,
			IdempotencyKey: schedule.IdempotencyKey(scheduledTime, row.Id),
			Message: &common.MessageOptions{
				Concurrency: row.Concurrency,
				Timeout:     row.Timeout,
			},
		})
		if err != nil {
			return err
		}
		if produced.Duplicate {
			// an earlier tick's ambiguous commit published this request, then
			// failed to advance the row
			i.Logger.WarnContext(ctx, schedule.EventJobRequestAlreadyPublished.Message, "code", schedule.EventJobRequestAlreadyPublished.Code, "schedule_id", row.Id, "schedule", row.Name, "scheduled_at", scheduledTime)
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
