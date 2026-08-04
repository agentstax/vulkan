package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

type ExceptionConsumerDefinition[Message any] struct {
	Config *ConsumerConfig
	Logger logger.Logger

	ds              *datastore.PostgresDatastore
	workers         *workercontroller.WorkerController
	abandonedEvents *consumermetrics.MetricEventProducer
	consumerFunc    ConsumerFunc[Message]
}

func NewExceptionConsumerDefinition[Message any](ds *datastore.PostgresDatastore, consumerFunc ConsumerFunc[Message], abandonedEvents *consumermetrics.MetricEventProducer, cfg *ConsumerConfig) (*ExceptionConsumerDefinition[Message], error) {
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}
	if consumerFunc == nil {
		return nil, errors.New("consumerFunc must not be nil")
	}
	if abandonedEvents == nil {
		return nil, errors.New("abandonedEvents producer must not be nil")
	}
	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	workers, err := workercontroller.NewWorkerController(ds, &workercontroller.ControllerConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &ExceptionConsumerDefinition[Message]{
		Config:          cfg,
		Logger:          cfg.Logger,
		ds:              ds,
		workers:         workers,
		abandonedEvents: abandonedEvents,
		consumerFunc:    consumerFunc,
	}, nil
}

func (f *ExceptionConsumerDefinition[Message]) Name() string {
	return WorkerExceptionConsumer
}

func (f *ExceptionConsumerDefinition[Message]) Declare(ctx context.Context, owner *common.Owner) error {
	return declareConsumerWorker(ctx, f.workers, WorkerExceptionConsumer, owner)
}

// a nil Execution is a declined claim, not an error -- try again later.
func (f *ExceptionConsumerDefinition[Message]) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	claimed, _, err := workercontroller.RegisterInstance[consumerWorkerMetadata](ctx, f.workers, workerId, owner, common.OwnerConsumerGroup, WorkerExceptionConsumer, metadata, f.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	return newExceptionConsumerExecution(ctx, f, owner, claimed)
}

type ExceptionConsumerExecution[Message any] struct {
	*consumerBase[Message]

	instanceRunner  *workercontroller.InstanceRunner
	exceptionRunner *exceptionRunner[Message]
}

func newExceptionConsumerExecution[Message any](ctx context.Context, definition *ExceptionConsumerDefinition[Message], owner *common.Owner, claimed *worker.WorkerInstance) (*ExceptionConsumerExecution[Message], error) {
	if definition == nil {
		return nil, errors.New("definition must not be nil")
	}
	if owner == nil {
		return nil, errors.New("owner must not be nil")
	}
	if claimed == nil {
		return nil, errors.New("claimed worker instance must not be nil")
	}

	base, err := newConsumerBase(ctx, definition.ds, owner, definition.consumerFunc, definition.abandonedEvents, definition.Config)
	if err != nil {
		return nil, err
	}
	exceptionRunner, err := newExceptionRunner(base)
	if err != nil {
		return nil, err
	}
	instanceRunner, err := workercontroller.NewInstanceRunner(definition.workers, claimed, &workercontroller.InstanceRunnerConfig{
		InstanceTTL: definition.Config.InstanceTTL,
		Logger:      logger.With(definition.Logger, "worker", WorkerExceptionConsumer, "owner", owner.Name),
	})
	if err != nil {
		return nil, err
	}

	return &ExceptionConsumerExecution[Message]{
		consumerBase:    base,
		instanceRunner:  instanceRunner,
		exceptionRunner: exceptionRunner,
	}, nil
}

func (i *ExceptionConsumerExecution[Message]) Run(ctx context.Context) error {
	return i.instanceRunner.Run(ctx, i.exceptionRunner.run)
}

type exceptionRunner[Message any] struct {
	*consumerBase[Message]
}

func newExceptionRunner[Message any](base *consumerBase[Message]) (*exceptionRunner[Message], error) {
	if base == nil {
		return nil, errors.New("base must not be nil")
	}
	return &exceptionRunner[Message]{consumerBase: base}, nil
}

func (r *exceptionRunner[Message]) run(ctx context.Context) error {
	ticker := time.NewTicker(r.Config.ClaimPollRate)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.exceptionClaim(ctx); err != nil {
				return err
			}
		}
	}
}

func (r *exceptionRunner[Message]) exceptionClaim(ctx context.Context) error {
	leaseDuration := r.Config.MessageMax.Timeout + r.Config.TimeoutGrace + r.Config.QueueMargin + r.Config.AckMargin

	// kill first, so an exhausted expired row is dead-lettered
	if err := r.Datastore.KillExceptions(ctx, r.Topic.Id, r.Group.Id, r.Config.MessageMax.Retry.MaxRetries, r.Topic.DisableDeliveryLog); err != nil {
		return err
	}

	claimed, err := r.Datastore.ClaimExceptions(ctx, r.Topic.Id, r.Group.Id, r.Config.BatchLimit, r.Config.MessageMax.Retry.MaxRetries, leaseDuration, r.Topic.DisableDeliveryLog)
	if err != nil {
		return err
	}

	for i := range claimed {
		if err := r.processException(ctx, &claimed[i]); err != nil {
			return err
		}
	}

	return nil
}

func (r *exceptionRunner[Message]) processException(ctx context.Context, exception *ClaimedException) error {
	resolvedOptions := r.Config.resolveMessageOptions(exception.Options)

	// sat behind the batch too long for the lease to cover a full run
	// try to renew it rather than start a run the lease can't protect
	leaseDuration := resolvedOptions.Timeout + r.Config.TimeoutGrace + r.Config.AckMargin
	if exception.LeaseUntil.Before(time.Now().Add(leaseDuration)) {
		renewed, err := r.Datastore.RenewExceptionLease(ctx, exception, leaseDuration)
		if err != nil {
			return err
		}
		if !renewed {
			r.Logger.DebugContext(ctx, "lease lost before the run started, re-claimed by another worker", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "message_id", exception.MessageId)
			return nil
		}
	}

	var keyClaim *KeyLeaseClaim
	if exception.CompactionKey != "" && resolvedOptions.Concurrency == common.ConcurrencyDefer {
		verdict, claim, err := r.claimKeyedRun(ctx, exception.CompactionKey, exception.MessageId, resolvedOptions)
		switch {
		case err != nil:
			// a failed key-lease claim counts as this attempt's own failure
			return r.recordFailure(ctx, exception, resolvedOptions, err, nil)
		case verdict == dispatchSuperseded:
			return r.recordSuperseded(ctx, exception)
		case verdict == dispatchDeferred:
			// our lease expired in the batch queue and another worker re-claimed it
			r.Logger.WarnContext(ctx, "key busy at gate, delivery re-claimed by another worker", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "message_id", exception.MessageId, "compaction_key", exception.CompactionKey)
			return nil
		}
		keyClaim = claim
	}

	var message Message
	if err := json.Unmarshal(exception.Payload, &message); err != nil {
		// bad payload will never deserialize -- no point retrying it
		return r.recordTerminal(ctx, exception, err, keyClaim)
	}

	if err := r.callSafely(withMeta(ctx, exception.toMessageMeta(resolvedOptions)), r.consumerFunc, &message, exception.MessageId, exception.Attempts, exception.Options, resolvedOptions.Timeout); err != nil {
		return r.recordFailure(ctx, exception, resolvedOptions, err, keyClaim)
	}
	return r.recordSuccess(ctx, exception, keyClaim)
}

// recordSuccess, recordFailure, recordTerminal, and recordSuperseded mirror
// the buffer's Resolve* verbs. A keyed run records on an uncancellable ctx:
// the key release is part of that same transaction and must land even
// mid-shutdown.
func (r *exceptionRunner[Message]) recordSuccess(ctx context.Context, exception *ClaimedException, keyClaim *KeyLeaseClaim) error {
	recordCtx := ctx
	if keyClaim != nil {
		var cancel context.CancelFunc
		recordCtx, cancel = context.WithTimeoutCause(context.WithoutCancel(ctx), r.Config.AckMargin,
			fmt.Errorf("outcome recording exceeded AckMargin (%s) for group %q topic %d", r.Config.AckMargin, r.consumerGroup, r.Topic.Id))
		defer cancel()
	}

	err := r.Datastore.RecordExceptionSuccess(recordCtx, exception, keyClaim)
	if errors.Is(err, ErrLeaseLost) {
		// reclaimed by another worker -- not ours to record anymore
		r.Logger.DebugContext(ctx, "lease lost recording exception outcome, re-claimed by another worker", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "message_id", exception.MessageId)
		return nil
	}
	return err
}

func (r *exceptionRunner[Message]) recordFailure(ctx context.Context, exception *ClaimedException, resolvedOptions *common.MessageOptions, runErr error, keyClaim *KeyLeaseClaim) error {
	recordCtx := ctx
	if keyClaim != nil {
		var cancel context.CancelFunc
		recordCtx, cancel = context.WithTimeoutCause(context.WithoutCancel(ctx), r.Config.AckMargin,
			fmt.Errorf("outcome recording exceeded AckMargin (%s) for group %q topic %d", r.Config.AckMargin, r.consumerGroup, r.Topic.Id))
		defer cancel()
	}

	err := r.Datastore.RecordExceptionFailure(recordCtx, resolvedOptions.Retry, exception, runErr, r.Topic.DisableDeliveryLog, keyClaim)
	if errors.Is(err, ErrLeaseLost) {
		r.Logger.DebugContext(ctx, "lease lost recording exception outcome, re-claimed by another worker", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "message_id", exception.MessageId)
		return nil
	}
	return err
}

func (r *exceptionRunner[Message]) recordTerminal(ctx context.Context, exception *ClaimedException, runErr error, keyClaim *KeyLeaseClaim) error {
	recordCtx := ctx
	if keyClaim != nil {
		var cancel context.CancelFunc
		recordCtx, cancel = context.WithTimeoutCause(context.WithoutCancel(ctx), r.Config.AckMargin,
			fmt.Errorf("outcome recording exceeded AckMargin (%s) for group %q topic %d", r.Config.AckMargin, r.consumerGroup, r.Topic.Id))
		defer cancel()
	}

	err := r.Datastore.RecordExceptionTerminal(recordCtx, exception, runErr, r.Topic.DisableDeliveryLog, keyClaim)
	if errors.Is(err, ErrLeaseLost) {
		r.Logger.DebugContext(ctx, "lease lost recording exception outcome, re-claimed by another worker", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "message_id", exception.MessageId)
		return nil
	}
	return err
}

func (r *exceptionRunner[Message]) recordSuperseded(ctx context.Context, exception *ClaimedException) error {
	err := r.Datastore.RecordExceptionSuperseded(ctx, exception, r.Topic.DisableDeliveryLog)
	if errors.Is(err, ErrLeaseLost) {
		r.Logger.DebugContext(ctx, "lease lost recording exception outcome, re-claimed by another worker", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "message_id", exception.MessageId)
		return nil
	}
	return err
}
