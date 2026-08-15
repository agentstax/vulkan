package cronscheduler

import (
	"context"
	"errors"
	"strconv"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/cron"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/agentstax/vulkan/pkg/worker"
	"github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/agentstax/vulkan/pkg/worker/cronscheduler/datastore"
	"github.com/google/uuid"
)

// scans cron_job for due rows at the row's poll_rate while a heartbeat holds
// the claim, producing one JobRequest per due row and advancing each produced
// row to its next scheduled time
type CronSchedulerExecution struct {
	Owner  *common.Owner
	Config *CronSchedulerConfig
	Logger logger.Logger

	runner    *controller.InstanceTickRunner
	datastore *datastore.CronSchedulerDatastore
	producer  *producer.Producer[cron.JobRequest]
	metadata  *cronSchedulerMetadata

	// registered by Run before the first scan -- scan never sees it nil
	producerInstance *producer.ProducerInstance[cron.JobRequest]
}

func newCronSchedulerExecution(cronScheduler *CronSchedulerDefinition, owner *common.Owner, claimed *worker.WorkerInstance, metadata *cronSchedulerMetadata) (*CronSchedulerExecution, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	runner, err := controller.NewInstanceTickRunner(cronScheduler.workers, claimed, metadata.PollRate.Effective(), &controller.InstanceTickRunnerConfig{
		InstanceTTL:    cronScheduler.Config.InstanceTTL,
		JitterFraction: cronScheduler.Config.JitterFraction,
		Logger:         logger.With(cronScheduler.Logger, "worker", WorkerCronScheduler, "system", owner.SystemId),
		TickRetry:      cronScheduler.Config.ScanRetry,
	})
	if err != nil {
		return nil, err
	}

	return &CronSchedulerExecution{
		Owner:     owner,
		Config:    cronScheduler.Config,
		Logger:    cronScheduler.Logger,
		runner:    runner,
		datastore: cronScheduler.datastore,
		producer:  cronScheduler.producer,
		metadata:  metadata,
	}, nil
}

// Run scans until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *CronSchedulerExecution) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "cron scheduler starting", "system", i.Owner.SystemId, "rate", i.metadata.PollRate.Effective())

	producerInstance, err := i.producer.Register(ctx, cron.TopicName, topic.SchemaVersion(1))
	if err != nil {
		return err
	}
	i.producerInstance = producerInstance

	err = i.runner.Run(ctx, i.scan)
	if err == nil {
		i.Logger.InfoContext(ctx, "cron scheduler stopped", "system", i.Owner.SystemId)
	}
	return err
}

// scan is one scheduler pass: an unlocked scan for due rows, then ONE
// transaction per row -- a shared transaction would let one bad row roll back
// every job's produce, and would hold ProduceInTx's whole-topic
// consumer-progress lock from the first produce to the end of the pass.
func (i *CronSchedulerExecution) scan(ctx context.Context) error {
	ids, err := i.datastore.DueCronJobs(ctx)
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
func (i *CronSchedulerExecution) produceJobRequest(ctx context.Context, id int64) error {
	return producer.InTransaction(ctx, i.datastore.Datastore, func(ctx context.Context, tx producer.Tx) error {
		row, err := i.datastore.ClaimDueCronJob(ctx, tx, id)
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
		passthrough := func(context.Context, producer.Tx, uuid.UUID) (*cron.JobRequest, error) {
			return request, nil
		}
		produced, err := i.producerInstance.ProduceInTx(ctx, tx, passthrough, producer.ProduceOptions{
			RoutingKey:     row.Name,
			CompactionKey:  strconv.FormatInt(row.Id, 10), // id not name -- a destroyed name's reuse must not share a key
			IdempotencyKey: cron.IdempotencyKey(scheduledTime, row.Id),
			Message: &common.MessageOptions{
				Concurrency: common.ConcurrencyPolicy(row.Concurrency),
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
			// park the row -- it has no honest next_scheduled_time
			i.Logger.WarnContext(ctx, "cron job schedule has no next scheduled time -- suspending", "cron_job", row.Id, "name", row.Name, "schedule", row.Schedule)
			return i.datastore.SuspendCronJob(ctx, tx, row.Id, scheduledTime)
		}
		return i.datastore.AdvanceCronJob(ctx, tx, row.Id, next, scheduledTime)
	})
}
