package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type MessageConsumerDefinition[Message any] struct {
	Config *ConsumerConfig
	Logger logger.Logger

	ds              *datastore.PostgresDatastore
	workers         *workercontroller.WorkerController
	abandonedEvents *consumermetrics.MetricEventProducer
	consumerFunc    ConsumerFunc[Message]
}

func NewMessageConsumerDefinition[Message any](ds *datastore.PostgresDatastore, consumerFunc ConsumerFunc[Message], abandonedEvents *consumermetrics.MetricEventProducer, cfg *ConsumerConfig) (*MessageConsumerDefinition[Message], error) {
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

	return &MessageConsumerDefinition[Message]{
		Config:          cfg,
		Logger:          cfg.Logger,
		ds:              ds,
		workers:         workers,
		abandonedEvents: abandonedEvents,
		consumerFunc:    consumerFunc,
	}, nil
}

func (f *MessageConsumerDefinition[Message]) Name() string {
	return WorkerMessageConsumer
}

func (f *MessageConsumerDefinition[Message]) Declare(ctx context.Context, owner *common.Owner) error {
	return declareConsumerWorker(ctx, f.workers, WorkerMessageConsumer, owner)
}

// a nil Execution is a declined claim, not an error -- try again later.
func (f *MessageConsumerDefinition[Message]) Provision(ctx context.Context, workerId int64, owner *common.Owner, metadata any) (worker.Execution, error) {
	claimed, _, err := workercontroller.RegisterInstance[consumerWorkerMetadata](ctx, f.workers, workerId, owner, common.OwnerConsumerGroup, WorkerMessageConsumer, metadata, f.Config.InstanceTTL)
	if err != nil || claimed == nil {
		return nil, err
	}

	return newMessageConsumerExecution(ctx, f, owner, claimed)
}

type MessageConsumerExecution[Message any] struct {
	*consumerBase[Message]

	instanceRunner *workercontroller.InstanceRunner
	messageRunner  *messageRunner[Message]
}

func newMessageConsumerExecution[Message any](ctx context.Context, definition *MessageConsumerDefinition[Message], owner *common.Owner, claimed *worker.WorkerInstance) (*MessageConsumerExecution[Message], error) {
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
	messageRunner, err := newMessageRunner(base)
	if err != nil {
		return nil, err
	}
	instanceRunner, err := workercontroller.NewInstanceRunner(definition.workers, claimed, &workercontroller.InstanceRunnerConfig{
		InstanceTTL: definition.Config.InstanceTTL,
		Logger:      logger.With(definition.Logger, "worker", WorkerMessageConsumer, "owner", owner.Name),
	})
	if err != nil {
		return nil, err
	}

	return &MessageConsumerExecution[Message]{
		consumerBase:   base,
		instanceRunner: instanceRunner,
		messageRunner:  messageRunner,
	}, nil
}

func (i *MessageConsumerExecution[Message]) Run(ctx context.Context) error {
	return i.instanceRunner.Run(ctx, i.messageRunner.run)
}

type messageRunner[Message any] struct {
	*consumerBase[Message]

	poolLimiter concurrency.PoolLimiter
	buffer      *claimBuffer
}

func newMessageRunner[Message any](base *consumerBase[Message]) (*messageRunner[Message], error) {
	if base == nil {
		return nil, errors.New("base must not be nil")
	}

	queue, err := concurrency.NewPressureQueue[Buffered](base.Config.QueueSize)
	if err != nil {
		return nil, err
	}
	poolLimiter, err := concurrency.NewWorkerPoolLimiter(base.Config.MessageConcurrency)
	if err != nil {
		return nil, err
	}
	buffer, err := NewClaimBuffer(queue)
	if err != nil {
		return nil, err
	}

	return &messageRunner[Message]{
		consumerBase: base,
		poolLimiter:  poolLimiter,
		buffer:       buffer,
	}, nil
}

// a fill side (prefetch) and a spend side (dispatch) run concurrently so the
// claim's network round trip overlaps whatever is being processed
func (r *messageRunner[Message]) run(ctx context.Context) error {
	// tracks in-flight goroutines independently of ctx, so a shutdown can
	// wait out stragglers instead of abandoning them the instant ctx cancels.
	var wg sync.WaitGroup

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return r.prefetch(gCtx)
	})
	g.Go(func() error {
		return r.dispatch(gCtx, &wg)
	})
	err := g.Wait()

	r.drain(ctx, &wg)
	r.closeOpenRanges(ctx)

	return err
}

// drain waits out in-flight work, bounded by ShutdownTimeout so a consumerFunc
// that ignores ctx.Done() can't hang shutdown forever. Whatever is still
// running past it is left for closeOpenRanges to settle.
func (r *messageRunner[Message]) drain(ctx context.Context, wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	timer := time.NewTimer(r.Config.ShutdownTimeout)
	defer timer.Stop()

	select {
	case <-done:
	case <-timer.C:
		r.Logger.WarnContext(ctx, "in-flight work did not finish before ShutdownTimeout, stragglers settle via lease expiry", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "shutdown_timeout", r.Config.ShutdownTimeout)
	}
}

// RemoveAll fences any straggler that resolves past this point, so this loop
// has sole ownership of every range it settles
func (r *messageRunner[Message]) closeOpenRanges(ctx context.Context) {
	for _, state := range r.buffer.RemoveAll() {
		r.closeRange(ctx, state)
	}
}

func (r *messageRunner[Message]) closeRange(ctx context.Context, state *rangeState) {
	if state.neverDispatched() && !state.stale.Load() {
		// surrendering beats making another worker wait out the whole lease for a
		// range nobody started. ctx is already Done by now, so this needs an
		// uncancelled one of its own to reach the database at all
		reclaimCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), r.Config.AckMargin,
			fmt.Errorf("force reclaim exceeded AckMargin (%s) for group %q topic %d", r.Config.AckMargin, r.consumerGroup, r.Topic.Id))
		defer cancel()

		if err := r.Datastore.ForceReclaimRange(reclaimCtx, r.Group.Id, state.lease.Token); err != nil && !errors.Is(err, ErrLeaseLost) {
			r.Logger.WarnContext(ctx, "force reclaim failed at shutdown, range rides out lease expiry instead", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "low", state.lease.Low, "high", state.lease.High, "err", err)
		}
		return
	}

	lastProcessed, exceptions, terminals, superseded, deferred := state.contiguousResolved()
	if err := r.cursorPartialCommit(ctx, lastProcessed, state.lease, exceptions, terminals, superseded, deferred); err != nil {
		r.Logger.WarnContext(ctx, "partial commit failed at shutdown, range rides out lease expiry instead", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "low", state.lease.Low, "high", state.lease.High, "err", err)
	}
}

func (r *messageRunner[Message]) prefetch(ctx context.Context) error {
	for {
		// blocks until there's room for a full batch, or the debounce timeout
		// elapses -- either way returns whatever room currently exists.
		room, err := r.buffer.WaitForRoom(ctx, r.Config.ClaimPollRate, r.Config.BatchLimit)
		if err != nil {
			return err
		}
		if room == 0 {
			continue
		}

		// worst-case -- a freshly claimed range always passes processClaim's
		// staleness check with the full QueueMargin left for queue wait.
		leaseDuration := r.Config.MessageMax.Timeout + r.Config.TimeoutGrace + r.Config.QueueMargin + r.Config.AckMargin
		limit := min(room, r.Config.BatchLimit)

		claimed, err := r.Datastore.ClaimMessagesWithCursor(ctx, r.Topic.Id, r.Group.Id, limit, r.Config.MaxRangeReclaims, leaseDuration, r.Topic.DisableDeliveryLog)
		if err != nil {
			// ctx cancellation is a real shutdown -> propagate and stop
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			// potential db blip -- back off instead of hot-looping the claim
			if err := SleepWithContext(ctx, r.Config.ClaimPollRate); err != nil {
				return err
			}
			continue
		}
		if claimed == nil {
			// caught up -- nothing to reclaim or claim
			if err := SleepWithContext(ctx, r.Config.ClaimPollRate); err != nil {
				return err
			}
			continue
		}

		if len(claimed.Messages) == 0 {
			// every message in the range compacted away -- nothing to dispatch or
			// resolve, so commit it directly to immediately move on
			r.commitRange(ctx, newRangeSnapshot(claimed.Lease, nil, nil, nil, nil))
			continue
		}
		if err := r.buffer.Add(ctx, claimed); err != nil {
			return err
		}
	}
}

func (r *messageRunner[Message]) dispatch(ctx context.Context, wg *sync.WaitGroup) error {
	for {
		owner, err := uuid.NewV7()
		if err != nil {
			return err // something is very wrong if this happens
		}

		if err := r.poolLimiter.WaitForPermit(ctx, owner.String()); err != nil {
			return err // ctx cancelled -- shutdown
		}

		item, err := r.buffer.WaitForNext(ctx)
		if err != nil {
			r.poolLimiter.ReleasePermit(ctx, owner.String()) // best effort
			return err                                       // ctx cancelled -- shutdown
		}

		wg.Go(func() {
			defer r.poolLimiter.ReleasePermit(ctx, owner.String())
			r.processClaim(ctx, item)
		})
	}
}

func (r *messageRunner[Message]) processClaim(ctx context.Context, item *Buffered) {
	resolvedOptions := r.Config.resolveMessageOptions(item.row.Options)

	// sat in the queue too long to safely start -- surrendering the whole
	// range beats risking a lease overrun (another worker reclaiming the
	// same range while this message is still being worked).
	if item.lease.Until.Before(time.Now().Add(resolvedOptions.Timeout + r.Config.TimeoutGrace + r.Config.AckMargin)) {
		r.buffer.MarkStale(item.lease.Token)
		return
	}

	r.runItem(ctx, item, resolvedOptions)

	if !r.buffer.IsRangeResolved(item.lease.Token) {
		return
	}
	snapshot, err := r.buffer.TryGetRangeSnapshot(item.lease.Token)
	if err != nil {
		return // another resolver or shutdown owns the commit
	}
	r.commitRange(ctx, snapshot)
}

// a message's concurrency policy can resolve it (superseded or deferred)
// without ever running consumerFunc
func (r *messageRunner[Message]) runItem(ctx context.Context, item *Buffered, resolvedOptions *common.MessageOptions) {
	if item.row.CompactionKey != "" && resolvedOptions.Concurrency == common.ConcurrencyDefer {
		verdict, claim, err := r.claimKeyedRun(ctx, item.row.CompactionKey, item.row.Id, resolvedOptions)
		switch {
		case err != nil:
			// record as an exception so it still runs later
			r.buffer.ResolveException(item, err)
			return
		case verdict == dispatchSuperseded:
			r.buffer.ResolveSuperseded(item)
			return
		case verdict == dispatchDeferred:
			r.buffer.ResolveDeferred(item)
			return
		}
		defer r.releaseKey(ctx, claim)
	}

	var message Message
	if err := json.Unmarshal(item.row.Payload, &message); err != nil {
		// bad payload will never deserialize -- no point retrying it
		r.buffer.ResolveTerminal(item, err)
		return
	}

	if err := r.callSafely(withMeta(ctx, item.row.toMessageMeta(resolvedOptions)), r.consumerFunc, &message, item.row.Id, 0, item.row.Options, resolvedOptions.Timeout); err != nil {
		r.buffer.ResolveException(item, err)
	} else {
		r.buffer.ResolveSuccess(item)
	}
}

func (r *messageRunner[Message]) releaseKey(ctx context.Context, claim *KeyLeaseClaim) {
	// runs after consumerFunc, when a shutdown may already have cancelled ctx
	releaseCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), r.Config.AckMargin,
		fmt.Errorf("key lease release exceeded AckMargin (%s) for group %q topic %d", r.Config.AckMargin, r.consumerGroup, r.Topic.Id))
	defer cancel()

	released, err := r.Datastore.ReleaseKeyLease(releaseCtx, claim)
	if err != nil {
		r.Logger.WarnContext(ctx, "key lease release failed, key frees on expiry instead", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "compaction_key", claim.CompactionKey, "err", err)
		return
	}
	if !released {
		// the run outlived its lease -- another delivery on the key may have
		// overlapped it
		r.Logger.WarnContext(ctx, "key lease expired mid-run and was taken over", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "compaction_key", claim.CompactionKey)
	}
}

func (r *messageRunner[Message]) commitRange(ctx context.Context, commit *rangeSnapshot) {
	// range always frees -- the lazy waterline roller advances committed
	// past it; failures ride along as parked exceptions, not a blocked range.
	err := r.Datastore.Commit(ctx, r.Topic.Id, r.Group.Id, commit.Lease.Token, commit.Exceptions, commit.Terminals, commit.Superseded, commit.Deferred, r.Config.ExceptionInitialBackoff, r.Topic.DisableDeliveryLog)
	switch {
	case err == nil:
		r.buffer.Remove(commit.Lease.Token)
	case errors.Is(err, ErrLeaseLost):
		r.Logger.DebugContext(ctx, "lease lost at commit, range re-claimed by another worker", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "low", commit.Lease.Low, "high", commit.Lease.High)
		r.buffer.Remove(commit.Lease.Token) // reclaimed mid-range -- the new owner processes it, not a failure here
	default:
		// stays tracked -- closeOpenRanges retries it on the way out
		r.Logger.WarnContext(ctx, "commit failed, range stays open for a retry at shutdown", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "low", commit.Lease.Low, "high", commit.Lease.High, "err", err)
	}
}

func (r *messageRunner[Message]) cursorPartialCommit(ctx context.Context, lastProcessed int64, lease LeaseRow, exceptions []MessageException, terminals []MessageTerminal, superseded []MessageSuperseded, deferred []MessageDeferred) error {
	if lastProcessed == lease.Low && len(exceptions) == 0 && len(terminals) == 0 && len(superseded) == 0 && len(deferred) == 0 {
		return nil // interrupted before resolving anything -- leave the lease exactly as claimed
	}

	// the ctx that got us here is already Done -- the commit needs its own
	// bounded, uncancelled window to actually reach the DB, same as Shutdown
	commitCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), r.Config.AckMargin,
		fmt.Errorf("partial commit exceeded AckMargin (%s) for group %q topic %d", r.Config.AckMargin, r.consumerGroup, r.Topic.Id))
	defer cancel()

	// narrow the lease to the untouched suffix instead of leaving the WHOLE
	// range (including the already-resolved prefix) to sit out a full reclaim.
	if err := r.Datastore.PartialCommit(commitCtx, r.Topic.Id, r.Group.Id, lease.Token, lastProcessed, exceptions, terminals, superseded, deferred, r.Config.ExceptionInitialBackoff, r.Topic.DisableDeliveryLog); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			r.Logger.DebugContext(ctx, "lease lost at partial commit, range re-claimed by another worker", "group", r.consumerGroup, "topic", r.Topic.Id, "version", r.version, "low", lease.Low, "high", lease.High)
			return nil // reclaimed mid-range -- the new owner processes it, not a failure here
		}
		// commitCtx expiring mid-call and PartialCommit's own DB error are
		// otherwise indistinguishable from the wire error alone
		if commitCtx.Err() != nil {
			return fmt.Errorf("%w: %w", err, context.Cause(commitCtx))
		}
		return err
	}
	return nil
}
