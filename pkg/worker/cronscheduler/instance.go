package cronscheduler

import (
	"context"
	"errors"
	"strconv"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	cronschedulercontroller "github.com/agentstax/vulkan/pkg/worker/cronscheduler/controller"
	"github.com/google/uuid"
)

// scans cron_job for due rows at the row's poll_rate while a heartbeat holds
// the claim, producing one JobRequest per due row and advancing each produced
// row to its next scheduled time
type CronSchedulerInstance struct {
	Owner  *common.Owner
	Config *CronSchedulerConfig
	Logger common.Logger

	runner           *controller.InstanceTickRunner
	ds               *iDatastore.PostgresDatastore
	controller       *cronschedulercontroller.CronSchedulerController
	metadata         *cronSchedulerMetadata
	producerInstance *producer.ProducerInstance[cron.JobRequest]
}

func newCronSchedulerInstance(cronScheduler *CronSchedulerProvisioner, owner *common.Owner, claimed *worker.WorkerInstance, metadata *cronSchedulerMetadata, producerInstance *producer.ProducerInstance[cron.JobRequest]) (*CronSchedulerInstance, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}
	if producerInstance == nil {
		return nil, errors.New("producerInstance must not be nil")
	}

	runner, err := controller.NewInstanceTickRunner(cronScheduler.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    cronScheduler.Config.InstanceTTL,
		JitterFraction: cronScheduler.Config.JitterFraction,
		Logger:         common.LoggerWith(cronScheduler.Logger, "worker", WorkerCronScheduler, "system", owner.SystemId),
		TickRetry:      cronScheduler.Config.ScanRetry,
	})
	if err != nil {
		return nil, err
	}

	return &CronSchedulerInstance{
		Owner:            owner,
		Config:           cronScheduler.Config,
		Logger:           cronScheduler.Logger,
		runner:           runner,
		ds:               cronScheduler.ds,
		controller:       cronScheduler.controller,
		metadata:         metadata,
		producerInstance: producerInstance,
	}, nil
}

// Run scans until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *CronSchedulerInstance) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "cron scheduler starting", "system", i.Owner.SystemId, "rate", i.metadata.PollRate)

	err := i.runner.Run(ctx, i.scan)
	if err == nil {
		i.Logger.InfoContext(ctx, "cron scheduler stopped", "system", i.Owner.SystemId)
	}
	return err
}

// scan is one scheduler pass: an unlocked scan for due rows, then ONE
// transaction per row -- a shared transaction would let one bad row roll back
// every job's produce, and would hold ProduceInTx's whole-topic
// consumer-progress lock from the first produce to the end of the pass.
func (i *CronSchedulerInstance) scan(ctx context.Context) error {
	ids, err := i.controller.ListDue(ctx)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := i.produceJobRequest(ctx, id); err != nil {
			if ctx.Err() != nil {
				return err
			}
			i.Logger.WarnContext(ctx, "cron job request produce failed -- siblings proceed", "cron_job", id, "error", err)
		}
	}
	return nil
}

// produceJobRequest resolves one due row: recheck under lock, produce the
// JobRequest for the NEWEST due scheduled time, advance the row. Produce +
// advance + idempotency claim share the transaction, so an ambiguous-commit
// replay rolls all three back together and the cron.IdempotencyKey dedupe
// covers exactly that replay.
func (i *CronSchedulerInstance) produceJobRequest(ctx context.Context, id int64) error {
	return producer.InTransaction(ctx, i.ds, func(ctx context.Context, tx producer.Tx) error {
		row, err := i.controller.ClaimDue(ctx, tx, id)
		if err != nil || row == nil {
			return err
		}

		schedule, err := cron.ParseSchedule(row.Schedule)
		if err != nil {
			return err
		}

		// produce the NEWEST due scheduled time -- after downtime, staleness
		// is at most one schedule rate; older due scheduled times are dropped,
		// not produced late. The IsZero guard keeps an unsatisfiable schedule
		// from spinning.
		scheduledTime := row.NextScheduledTime
		for next := schedule.Next(scheduledTime); !next.IsZero() && !next.After(row.DbNow); next = schedule.Next(scheduledTime) {
			scheduledTime = next
		}

		request, err := cron.NewJobRequest(row.Id, row.Name, scheduledTime, row.Data, row.Metadata)
		if err != nil {
			return err
		}

		// id not name -- a destroyed name's reuse must not share a key
		compaction, err := producer.NewCompactionOptions(strconv.FormatInt(row.Id, 10), 0)
		if err != nil {
			return err
		}

		passthrough := func(context.Context, producer.Tx, uuid.UUID) (*cron.JobRequest, error) {
			return request, nil
		}
		produced, err := i.producerInstance.ProduceInTx(ctx, tx, passthrough, producer.ProduceOptions{
			RoutingKey:     row.Name,
			Compaction:     compaction,
			IdempotencyKey: cron.IdempotencyKey(scheduledTime, row.Id),
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
			i.Logger.WarnContext(ctx, "cron job request was already published by an earlier ambiguous commit", "cron_job", row.Id, "name", row.Name, "scheduled_time", scheduledTime)
		}

		// next scheduled time from the DB clock ONLY -- Go/DB skew
		// double-produces tight schedules
		next := schedule.Next(row.DbNow)
		if next.IsZero() {
			// schedule went unsatisfiable (tzdata drift): keep the produce,
			// suspend the row -- it has no honest next_scheduled_time
			i.Logger.WarnContext(ctx, "cron job schedule has no next scheduled time -- suspending", "cron_job", row.Id, "name", row.Name, "schedule", row.Schedule)
			return i.controller.Suspend(ctx, tx, row.Id, scheduledTime)
		}
		return i.controller.Advance(ctx, tx, row.Id, next, scheduledTime)
	})
}
