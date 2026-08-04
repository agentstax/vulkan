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
// the claim, producing one JobRequest per due firing and advancing each fired
// row to its next firing
type CronSchedulerExecution struct {
	Owner  *common.Owner
	Config *CronSchedulerConfig
	Logger logger.Logger

	runner    *controller.InstanceTickRunner
	datastore *datastore.CronSchedulerDatastore
	producer  *producer.Producer[cron.JobRequest]
	metadata  *cronSchedulerMetadata
}

func newCronSchedulerExecution(cronScheduler *CronSchedulerDefinition, owner *common.Owner, claimed *worker.WorkerInstance, metadata *cronSchedulerMetadata) (*CronSchedulerExecution, error) {
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if metadata == nil {
		return nil, errors.New("metadata must not be nil")
	}

	runner, err := controller.NewInstanceTickRunner(cronScheduler.workers, claimed, metadata.PollRate, &controller.InstanceTickRunnerConfig{
		InstanceTTL:    cronScheduler.Config.InstanceTTL,
		JitterFraction: cronScheduler.Config.JitterFraction,
		Logger:         logger.With(cronScheduler.Logger, "worker", WorkerCronScheduler, "system", owner.SystemId),
		TickRetry:      cronScheduler.Config.ScanRetry,
	})
	if err != nil {
		return nil, err
	}

	// a producer's lifecycle ends with its Register ctx and stays down, so
	// every instance owns a fresh one
	jobProducer, err := producer.NewProducer[cron.JobRequest](cron.TopicName, topic.SchemaVersion(1), cronScheduler.ds, &producer.ProducerConfig{
		Logger: cronScheduler.Logger,
		Retry:  cronScheduler.Config.Retry,
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
		producer:  jobProducer,
		metadata:  metadata,
	}, nil
}

// Run scans until ctx cancels; a requested stop returns nil. The claimed
// instance releases on the way out however Run exits.
func (i *CronSchedulerExecution) Run(ctx context.Context) error {
	i.Logger.InfoContext(ctx, "cron scheduler starting", "system", i.Owner.SystemId, "rate", i.metadata.PollRate)

	if err := i.producer.Register(ctx); err != nil {
		return err
	}

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
			i.Logger.WarnContext(ctx, "cron job firing failed -- siblings proceed", "cron_job", id, "error", err)
		}
	}
	return nil
}

// produceJobRequest fires one due row: recheck under lock, produce the NEWEST
// due firing, advance the row. Produce + advance + idempotency claim share the
// transaction, so an ambiguous-commit replay rolls all three back together and
// the FiringKey dedupe covers exactly that replay.
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

		// fire the NEWEST due firing -- after downtime, staleness is at most
		// one firing rate; older due firings are dropped, not fired late. The
		// IsZero guard keeps an unsatisfiable schedule from spinning.
		firing := row.NextScheduledTime
		for next := schedule.Next(firing); !next.IsZero() && !next.After(row.DbNow); next = schedule.Next(firing) {
			firing = next
		}

		request, err := cron.NewJobRequest(row.Id, row.Name, firing, row.Data, row.Metadata)
		if err != nil {
			return err
		}
		passthrough := func(context.Context, producer.Tx, uuid.UUID) (*cron.JobRequest, error) {
			return request, nil
		}
		_, err = i.producer.ProduceInTx(ctx, tx, passthrough, producer.ProduceOptions{
			RoutingKey:     row.Name,
			CompactionKey:  strconv.FormatInt(row.Id, 10), // id not name -- a destroyed name's reuse must not share a key
			IdempotencyKey: cron.FiringKey(firing, row.Id),
			Message: &common.MessageOptions{
				Concurrency: common.ConcurrencyPolicy(row.Concurrency),
				Timeout:     row.Timeout,
			},
		})
		if err != nil {
			return err
		}

		// next firing from the DB clock ONLY -- Go/DB skew double-fires tight schedules
		next := schedule.Next(row.DbNow)
		if next.IsZero() {
			// schedule went unsatisfiable (tzdata drift): keep the produce,
			// park the row -- it has no honest next_scheduled_time
			i.Logger.WarnContext(ctx, "cron job schedule has no next firing -- suspending", "cron_job", row.Id, "name", row.Name, "schedule", row.Schedule)
			return i.datastore.SuspendCronJob(ctx, tx, row.Id, firing)
		}
		return i.datastore.AdvanceCronJob(ctx, tx, row.Id, next, firing)
	})
}
