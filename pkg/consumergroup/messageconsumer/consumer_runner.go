package messageconsumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumerbase "github.com/agentstax/vulkan/pkg/consumergroup/base"
	keyleasecontroller "github.com/agentstax/vulkan/pkg/consumergroup/base/controller"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

type messageRunner[Message any] struct {
	*consumerbase.BaseConsumer[Message]

	consumers   *controller.MessageConsumerGroupController
	cfg         *MessageConsumerConfig
	poolLimiter concurrency.PoolLimiter
	buffer      *claimBuffer
}

func newMessageRunner[Message any](base *consumerbase.BaseConsumer[Message], consumers *controller.MessageConsumerGroupController, cfg *MessageConsumerConfig) (*messageRunner[Message], error) {
	if base == nil {
		return nil, errors.New("base must not be nil")
	}
	if consumers == nil {
		return nil, errors.New("consumers controller must not be nil")
	}
	if cfg == nil {
		return nil, errors.New("config must not be nil")
	}

	queue, err := concurrency.NewPressureQueue[buffered](cfg.QueueSize)
	if err != nil {
		return nil, err
	}
	poolLimiter, err := concurrency.NewWorkerPoolLimiter(cfg.MessageConcurrency)
	if err != nil {
		return nil, err
	}

	// only DeliveryLogModeAll wants success outcomes collected at commit
	buffer, err := newClaimBuffer(queue, base.Topic.DeliveryLogMode == topic.DeliveryLogModeAll)
	if err != nil {
		return nil, err
	}

	return &messageRunner[Message]{
		BaseConsumer: base,
		consumers:    consumers,
		cfg:          cfg,
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

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		return r.prefetch(groupCtx)
	})
	group.Go(func() error {
		return r.dispatch(groupCtx, &wg)
	})
	err := group.Wait()

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

	timer := time.NewTimer(r.cfg.ShutdownTimeout)
	defer timer.Stop()

	select {
	case <-done:
	case <-timer.C:
		r.Logger.WarnContext(ctx, "in-flight work did not finish before the shutdown timeout -- stragglers settle via lease expiry", "group", r.Owner.Name, "topic_id", r.Topic.Id, "version", r.Topic.SchemaVersion, "shutdown_timeout", r.cfg.ShutdownTimeout)
	}
}

// RemoveAll fences any straggler that resolves past this point, so this loop
// has sole ownership of every range it settles
func (r *messageRunner[Message]) closeOpenRanges(ctx context.Context) {
	for _, state := range r.buffer.removeAll() {
		r.closeRange(ctx, state)
	}
}

func (r *messageRunner[Message]) closeRange(ctx context.Context, state *rangeState) {
	if state.neverDispatched() && !state.stale.Load() {
		// surrendering beats making another worker wait out the whole lease for a
		// range nobody started. ctx is already Done by now, so this needs an
		// uncancelled one of its own to reach the database at all
		reclaimCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), r.cfg.RecordMargin,
			fmt.Errorf("force reclaim exceeded RecordMargin (%s) for group %q topic %d", r.cfg.RecordMargin, r.Owner.Name, r.Topic.Id))
		defer cancel()

		if err := r.consumers.ForceReclaimRange(reclaimCtx, r.Topic.Id, r.Owner.ConsumerGroupId, state.lease.Token); err != nil && !errors.Is(err, common.ErrLeaseLost) {
			r.Logger.WarnContext(ctx, "could not force reclaim at shutdown -- range rides out lease expiry", "group", r.Owner.Name, "topic_id", r.Topic.Id, "low", state.lease.Low, "high", state.lease.High, "error", err)
		}
		return
	}

	lastProcessed, outcomes := state.contiguousResolved()
	if err := r.cursorPartialCommit(ctx, lastProcessed, state.lease, outcomes); err != nil {
		r.Logger.WarnContext(ctx, "partial commit did not complete at shutdown -- range rides out lease expiry", "group", r.Owner.Name, "topic_id", r.Topic.Id, "low", state.lease.Low, "high", state.lease.High, "error", err)
	}
}

func (r *messageRunner[Message]) prefetch(ctx context.Context) error {
	for {
		// blocks until there's room for a full batch, or the debounce timeout
		// elapses -- either way returns whatever room currently exists.
		room, err := r.buffer.waitForRoom(ctx, r.cfg.ClaimPollRate, r.cfg.BatchLimit)
		if err != nil {
			return err
		}
		if room == 0 {
			continue
		}

		// worst-case -- a freshly claimed range always passes processClaim's
		// staleness check with the full QueueMargin left for queue wait.
		leaseDuration := r.cfg.MessageMax.Timeout + r.cfg.TimeoutGrace + r.cfg.QueueMargin + r.cfg.RecordMargin
		limit := min(room, r.cfg.BatchLimit)

		claimed, err := r.consumers.ClaimMessagesWithCursor(ctx, r.Topic.Id, r.Owner.ConsumerGroupId, limit, r.cfg.MaxRangeReclaims, leaseDuration, r.Topic.DeliveryLogMode)
		if err != nil {
			// ctx cancellation is a real shutdown -> propagate and stop
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}

			// potential db blip -- back off instead of hot-looping the claim
			if err := sleepWithContext(ctx, r.cfg.ClaimPollRate); err != nil {
				return err
			}
			continue
		}
		if claimed == nil {
			// caught up -- nothing to reclaim or claim
			if err := sleepWithContext(ctx, r.cfg.ClaimPollRate); err != nil {
				return err
			}
			continue
		}

		// a fresh claim's lease starts at 0 reclaims; anything above marks a
		// range taken over from an expired worker
		if claimed.Lease.Reclaims > 0 {
			r.Metrics.RecordReclaimed(1)
		}
		if claimed.Quarantined {
			r.Metrics.RecordQuarantined(1)
			continue
		}
		r.Metrics.RecordClaimed(len(claimed.Messages))

		if len(claimed.Messages) == 0 {
			// every message in the range compacted away -- nothing to dispatch or
			// resolve, so commit it directly to immediately move on
			r.commitRange(ctx, newRangeSnapshot(claimed.Lease, nil))
			continue
		}
		if err := r.buffer.add(ctx, claimed); err != nil {
			return err
		}
	}
}

func (r *messageRunner[Message]) dispatch(ctx context.Context, wg *sync.WaitGroup) error {
	for {
		permitOwner, err := uuid.NewV7()
		if err != nil {
			return err // something is very wrong if this happens
		}

		if err := r.poolLimiter.WaitForPermit(ctx, permitOwner.String()); err != nil {
			return err // ctx cancelled -- shutdown
		}

		item, err := r.buffer.waitForNext(ctx)
		if err != nil {
			r.poolLimiter.ReleasePermit(ctx, permitOwner.String()) // best effort
			return err                                             // ctx cancelled -- shutdown
		}

		wg.Go(func() {
			defer r.poolLimiter.ReleasePermit(ctx, permitOwner.String())
			r.processClaim(ctx, item)
		})
	}
}

func (r *messageRunner[Message]) processClaim(ctx context.Context, item *buffered) {
	resolvedOptions := r.cfg.resolveMessageOptions(item.row.Options)

	// sat in the queue too long to safely start -- surrendering the whole
	// range beats risking a lease overrun (another worker reclaiming the
	// same range while this message is still being worked).
	if item.lease.ExpiresAt.Before(time.Now().Add(resolvedOptions.Timeout + r.cfg.TimeoutGrace + r.cfg.RecordMargin)) {
		r.buffer.markStale(item.lease.Token)
		return
	}

	r.runItem(ctx, item, resolvedOptions)

	if !r.buffer.isRangeResolved(item.lease.Token) {
		return
	}
	snapshot, err := r.buffer.tryGetRangeSnapshot(item.lease.Token)
	if err != nil {
		return // another resolver or shutdown owns the commit
	}
	r.commitRange(ctx, snapshot)
}

// a message's concurrency policy can resolve it (superseded or deferred)
// without ever running consumerFunc
func (r *messageRunner[Message]) runItem(ctx context.Context, item *buffered, resolvedOptions *common.MessageOptions) {
	if item.row.MessageKey != "" && resolvedOptions.Concurrency == common.ConcurrencyExclusive {
		claim, err := r.ClaimKeyedRun(ctx, item.row.MessageKey, item.row.Id, item.row.Compacted, resolvedOptions)
		switch {
		case err != nil:
			// record as an exception so it still runs later
			r.buffer.resolveException(item, err)
			return
		case claim.Verdict == keyleasecontroller.KeyLeaseSuperseded:
			r.Metrics.RecordSuperseded(1)
			r.buffer.resolveSuperseded(item)
			return
		case claim.Verdict == keyleasecontroller.KeyLeaseBusy:
			// another delivery holds the key -- the range commit writes its
			// 'deferred' row
			r.buffer.resolveDeferred(item)
			return
		}
		defer r.releaseKey(ctx, claim)
	}

	var payload Message
	if err := json.Unmarshal(item.row.Payload, &payload); err != nil {
		// bad payload will never deserialize -- no point retrying it
		r.buffer.resolveTerminal(item, err)
		return
	}

	runCtx := consumergroup.WithMeta(ctx, toMessageMeta(item.row, resolvedOptions))
	err := r.CallSafely(runCtx, &payload, item.row.Id, 0, item.row.Options, resolvedOptions.Timeout)

	switch consumerbase.ClassifyHandlerError(err) {
	case consumerbase.HandlerOutcomeTerminal:
		r.buffer.resolveTerminal(item, err)
		return
	case consumerbase.HandlerOutcomeDelayed:
		// a first delivery has no delays yet, so MaxDelays cannot be reached here
		delayed, _ := errors.AsType[*consumergroup.DelayedDelivery](err)
		r.buffer.resolveDelayed(item, delayed)
		return
	}
	if err != nil {
		r.buffer.resolveException(item, err)
		return
	}

	r.Metrics.RecordSuccess(1)
	r.buffer.resolveSuccess(item)
}

func (r *messageRunner[Message]) releaseKey(ctx context.Context, claim *keyleasecontroller.KeyLeaseClaim) {
	// runs after consumerFunc, when a shutdown may already have cancelled ctx
	releaseCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), r.cfg.RecordMargin,
		fmt.Errorf("key lease release exceeded RecordMargin (%s) for group %q topic %d", r.cfg.RecordMargin, r.Owner.Name, r.Topic.Id))
	defer cancel()

	released, err := r.ReleaseKeyedRun(releaseCtx, claim)
	if err != nil {
		r.Logger.WarnContext(ctx, "could not release key lease -- key frees on expiry", "group", r.Owner.Name, "topic_id", r.Topic.Id, "message_key", claim.MessageKey, "error", err)
		return
	}
	if !released {
		// the run outlived its lease -- another delivery on the key may have
		// overlapped it
		r.Logger.WarnContext(ctx, "key lease expired mid-run and was taken over", "group", r.Owner.Name, "topic_id", r.Topic.Id, "message_key", claim.MessageKey)
	}
}

func (r *messageRunner[Message]) commitRange(ctx context.Context, commit *rangeSnapshot) {
	// range always frees -- the cursor advancer advances committed
	// past it; failures become unresolved exceptions, not a blocked range.
	err := r.consumers.Commit(ctx, r.Topic.Id, r.Owner.ConsumerGroupId, commit.Lease.Token, commit.Outcomes, r.cfg.ExceptionInitialBackoff, r.Topic.DeliveryLogMode)
	switch {
	case err == nil:
		r.countDeliveryRows(commit.Outcomes)
		r.buffer.remove(commit.Lease.Token)
	case errors.Is(err, common.ErrLeaseLost):
		r.Metrics.RecordLeaseLost(1)
		r.Logger.DebugContext(ctx, "lease lost at commit -- range re-claimed by another worker", "group", r.Owner.Name, "topic_id", r.Topic.Id, "low", commit.Lease.Low, "high", commit.Lease.High)
		r.buffer.remove(commit.Lease.Token) // reclaimed mid-range -- the new owner processes it, not a failure here
	default:
		// stays tracked -- closeOpenRanges retries it on the way out
		r.Logger.WarnContext(ctx, "could not commit -- range stays open for a retry at shutdown", "group", r.Owner.Name, "topic_id", r.Topic.Id, "low", commit.Lease.Low, "high", commit.Lease.High, "error", err)
	}
}

func (r *messageRunner[Message]) cursorPartialCommit(ctx context.Context, lastProcessed int64, lease controller.RangeLease, outcomes []controller.MessageOutcome) error {
	if lastProcessed == lease.Low && len(outcomes) == 0 {
		return nil // interrupted before resolving anything -- leave the lease exactly as claimed
	}

	// the ctx that got us here is already Done -- the commit needs its own
	// bounded, uncancelled window to actually reach the DB, same as Shutdown
	commitCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), r.cfg.RecordMargin,
		fmt.Errorf("partial commit exceeded RecordMargin (%s) for group %q topic %d", r.cfg.RecordMargin, r.Owner.Name, r.Topic.Id))
	defer cancel()

	// narrow the lease to the untouched suffix instead of leaving the WHOLE
	// range (including the already-resolved prefix) to sit out a full reclaim.
	if err := r.consumers.PartialCommit(commitCtx, r.Topic.Id, r.Owner.ConsumerGroupId, lease.Token, lastProcessed, outcomes, r.cfg.ExceptionInitialBackoff, r.Topic.DeliveryLogMode); err != nil {
		if errors.Is(err, common.ErrLeaseLost) {
			r.Metrics.RecordLeaseLost(1)
			r.Logger.DebugContext(ctx, "lease lost at partial commit -- range re-claimed by another worker", "group", r.Owner.Name, "topic_id", r.Topic.Id, "low", lease.Low, "high", lease.High)
			return nil // reclaimed mid-range -- the new owner processes it, not a failure here
		}

		// commitCtx expiring mid-call and PartialCommit's own DB error are
		// otherwise indistinguishable from the wire error alone
		if commitCtx.Err() != nil {
			return fmt.Errorf("%w: %w", err, context.Cause(commitCtx))
		}
		return err
	}

	r.countDeliveryRows(outcomes)
	return nil
}

// countDeliveryRows bumps the session counters for the delivery rows a landed
// commit wrote. Success and superseded write no delivery row and are counted
// at resolution instead.
func (r *messageRunner[Message]) countDeliveryRows(outcomes []controller.MessageOutcome) {
	for _, outcome := range outcomes {
		switch outcome.Kind {
		case controller.OutcomeException, controller.OutcomeDelayed:
			r.Metrics.RecordReady(1)
		case controller.OutcomeDeferred:
			r.Metrics.RecordDeferred(1)
		case controller.OutcomeTerminal:
			r.Metrics.RecordDead(1)
		}
	}
}
