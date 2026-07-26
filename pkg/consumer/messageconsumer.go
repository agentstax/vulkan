package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer/metrics"
	"github.com/agentstax/vulkan/pkg/datastore"
	vulkanerrors "github.com/agentstax/vulkan/pkg/errors"
	"github.com/agentstax/vulkan/pkg/maintain"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// MessageConsumer is the bare message work loop: claim ranges off the group's
// cursor, dispatch across PoolLimiter's N processors, commit. No exception
// retries, no maintenance -- run Consumer instead for all of that, or compose
// the pieces (ExceptionConsumer, pkg/maintain) yourself.
type MessageConsumer[Message any] struct {
	consumerBase[Message]
	PoolLimiter concurrency.PoolLimiter

	buffer      *claimBuffer                   // wraps the queue given to NewMessageConsumer -- nothing outside processCursor needs the raw queue
	maintenance *maintain.MaintenanceDatastore // cold-start partition create only, never duty work
}

// cfg may be nil or a sparse struct -- WithDefaults fills every field left
// unset, Validate rejects what's out of range.
func NewMessageConsumer[Message any](consumerGroup string, topicName string, queue concurrency.Queue[Buffered], poolLimiter concurrency.PoolLimiter, ds *datastore.PostgresDatastore, cfg *ConsumerConfig) (*MessageConsumer[Message], error) {
	if topicName == "" {
		return nil, errors.New("topic name is required")
	}
	if queue == nil {
		return nil, errors.New("queue must not be nil")
	}
	if poolLimiter == nil {
		return nil, errors.New("poolLimiter must not be nil")
	}
	if ds == nil {
		return nil, errors.New("datastore must not be nil")
	}

	if cfg == nil {
		cfg = &ConsumerConfig{}
	}
	cfg.WithDefaults()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Prefetcher can work around this with debounce timeout however
	// having your queue smaller than batch limit seems like a code smell so error for now
	if queue.Cap() < cfg.BatchLimit {
		return nil, fmt.Errorf("queue cap (%d) must be >= BatchLimit (%d), otherwise prefetcher can never claim a full batch", queue.Cap(), cfg.BatchLimit)
	}

	base, err := newConsumerBase[Message](consumerGroup, topicName, ds, cfg)
	if err != nil {
		return nil, err
	}

	buffer, err := NewClaimBuffer(queue)
	if err != nil {
		return nil, err
	}

	maintenanceDatastore, err := maintain.NewMaintenanceDatastore(ds, &maintain.MaintenanceDatastoreConfig{
		Logger: cfg.Logger,
		Retry:  cfg.Retry,
	})
	if err != nil {
		return nil, err
	}

	return &MessageConsumer[Message]{
		consumerBase: base,
		PoolLimiter:  poolLimiter,
		buffer:       buffer,
		maintenance:  maintenanceDatastore,
	}, nil
}

// Register resolves this consumer's topic by name against the live topic row,
// sets up its cursor, and starts the consumer's lifecycle.
//
// ctx must be cancellable, unless ConsumerConfig.DisableGracefulShutdown
// declares otherwise.
func (p *MessageConsumer[Message]) Register(ctx context.Context) error {
	if err := p.register(ctx); err != nil {
		return err
	}

	consumerMetrics, err := metrics.NewConsumerMetrics(p.Config.Meter, p.consumerGroup, p.Topic.Id, p.Topic.Name, p.Datastore.Datastore, &metrics.ConsumerMetricsDatastoreConfig{
		Logger: p.Config.Logger,
	})
	if err != nil {
		return err
	}
	p.Metrics = consumerMetrics

	// cold-start guarantee: the next partition exists before the janitor
	// duty's first (jittered) tick
	if err := p.maintenance.EnsureNextPartition(ctx, p.Topic.Id, p.Topic.PartitionSize); err != nil {
		return err
	}

	// tracked for graceful shutdown draining / handling
	p.lifecycleCtx = ctx

	return nil
}

// Consume claims and processes messages with consumerFunc, blocking until
// stopped: cancel ctx to stop this call, or cancel the context given to
// Register to wind the whole consumer down. A requested stop from either side
// shuts down in-flight work and returns nil
func (p *MessageConsumer[Message]) Consume(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	if err := p.lifecycleErr(); err != nil {
		return err
	}
	runCtx, cancel := p.runCtx(ctx)
	defer cancel()

	p.Logger.InfoContext(runCtx, "message consumer starting", "group", p.consumerGroup, "topic", p.Topic.Id)

	err := p.processCursor(runCtx, consumerFunc)
	if errors.Is(err, context.Canceled) {
		// requested shutdown (either side), not a failure -- log which side asked
		reason := "caller context cancelled"
		if errors.Is(context.Cause(runCtx), vulkanerrors.ErrShutdownRequested) {
			reason = "lifecycle context cancelled"
		}
		p.Logger.InfoContext(ctx, "message consumer stopped", "reason", reason, "group", p.consumerGroup, "topic", p.Topic.Id)
		err = nil
	}
	return err
}

// processCursor has a fill side (prefetch) and a spend side (dispatch) running concurrently.
// This allows the claim's network round trip to overlap whatever is being processed.
func (p *MessageConsumer[Message]) processCursor(ctx context.Context, consumerFunc ConsumerFunc[Message]) error {
	// tracks in-flight goroutines independently of ctx, so a shutdown can
	// wait out stragglers instead of abandoning them the instant ctx cancels.
	var wg sync.WaitGroup

	g, gCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		return p.prefetch(gCtx)
	})
	g.Go(func() error {
		return p.dispatch(gCtx, &wg, consumerFunc)
	})
	err := g.Wait()

	p.Drain(ctx, &wg)
	p.CloseOpenRanges(ctx)

	return err
}

// Drain waits for in-flight processClaim calls to finish, bounded by
// ShutdownTimeout -- a consumerFunc that ignores ctx.Done() can't hang
// shutdown forever; whatever's still running past the timeout is left for
// CloseOpenRanges to settle instead.
func (p *MessageConsumer[Message]) Drain(ctx context.Context, wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() {
		wg.Wait() // this is what waits for in-flight work to complete
		close(done)
	}()

	timer := time.NewTimer(p.Config.ShutdownTimeout)
	defer timer.Stop()

	select {
	case <-done:
	case <-timer.C:
		p.Logger.WarnContext(ctx, "in-flight work did not finish before ShutdownTimeout, stragglers settle via lease expiry", "group", p.consumerGroup, "topic", p.Topic.Id, "shutdown_timeout", p.Config.ShutdownTimeout)
	}
}

// CloseOpenRanges takes sole ownership of every range still tracked after
// Drain (RemoveAll fences any straggler that resolves after this point) and
// settles each: untouched ranges surrender immediately via ForceReclaimRange
// so another worker doesn't wait out a full lease for nothing; anything with
// resolved progress commits that prefix via CursorPartialCommit.
func (p *MessageConsumer[Message]) CloseOpenRanges(ctx context.Context) {
	for _, state := range p.buffer.RemoveAll() {
		p.closeRange(ctx, state)
	}
}

func (p *MessageConsumer[Message]) closeRange(ctx context.Context, state *rangeState) {
	if state.neverDispatched() && !state.stale.Load() {
		// ctx is already Done at shutdown need fresh to complete graceful shutdown.
		// CursorPartialCommit constructs new context internally.
		reclaimCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), p.Config.AckMargin,
			fmt.Errorf("force reclaim exceeded AckMargin (%s) for group %q topic %d", p.Config.AckMargin, p.consumerGroup, p.Topic.Id))
		defer cancel()

		if err := p.Datastore.ForceReclaimRange(reclaimCtx, p.Topic.Id, p.consumerGroup, state.lease.Token); err != nil && !errors.Is(err, ErrLeaseLost) {
			p.Logger.WarnContext(ctx, "force reclaim failed at shutdown, range rides out lease expiry instead", "group", p.consumerGroup, "topic", p.Topic.Id, "low", state.lease.Low, "high", state.lease.High, "err", err)
		}
		return
	}

	lastProcessed, exceptions, terminals := state.contiguousResolved()
	if err := p.CursorPartialCommit(ctx, lastProcessed, state.lease, exceptions, terminals); err != nil {
		p.Logger.WarnContext(ctx, "partial commit failed at shutdown, range rides out lease expiry instead", "group", p.consumerGroup, "topic", p.Topic.Id, "low", state.lease.Low, "high", state.lease.High, "err", err)
	}
}

// prefetch claims ranges and feeds their messages into p.buffer.
func (p *MessageConsumer[Message]) prefetch(ctx context.Context) error {
	for {
		// blocks until there's room for a full batch, or the debounce timeout
		// elapses -- either way returns whatever room currently exists.
		room, err := p.buffer.WaitForRoom(ctx, p.Config.ClaimPollRate, p.Config.BatchLimit)
		if err != nil {
			return err
		}
		if room == 0 {
			continue
		}

		// leaseDuration should always have an extra time buffer (QueueMargin) to not
		// potentially overlap with another worker reclaiming (double processing)
		leaseDuration := p.Config.WorkTimeout + p.Config.QueueMargin + p.Config.AckMargin
		limit := min(room, p.Config.BatchLimit)

		claimed, err := p.Datastore.ClaimMessagesWithCursor(ctx, p.Topic.Id, p.consumerGroup, limit, p.Config.MaxRangeReclaims, leaseDuration, p.Topic.DisableDeliveryLog)
		if err != nil {
			// ctx cancellation is a real shutdown -> propagate and stop
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			// potential db blip -- back off instead of hot-looping the claim
			if err := SleepWithContext(ctx, p.Config.ClaimPollRate); err != nil {
				return err
			}
			continue
		}
		if claimed == nil {
			// caught up -- nothing to reclaim or claim
			if err := SleepWithContext(ctx, p.Config.ClaimPollRate); err != nil {
				return err
			}
			continue
		}

		if len(claimed.Messages) == 0 {
			// every message in the range compacted away -- nothing to dispatch or
			// resolve, so commit it directly to immediately move on
			p.commitRange(ctx, newRangeSnapshot(claimed.Lease, nil, nil))
			continue
		}
		if err := p.buffer.Add(ctx, claimed); err != nil {
			return err
		}
	}
}

// dispatch drains p.buffer across PoolLimiter's N concurrent processors
func (p *MessageConsumer[Message]) dispatch(ctx context.Context, wg *sync.WaitGroup, consumerFunc ConsumerFunc[Message]) error {
	for {
		owner, err := uuid.NewV7()
		if err != nil {
			return err // something is very wrong if this happens
		}

		if err := p.PoolLimiter.WaitForPermit(ctx, owner.String()); err != nil {
			return err // ctx cancelled -- shutdown
		}

		item, err := p.buffer.WaitForNext(ctx)
		if err != nil {
			p.PoolLimiter.ReleasePermit(ctx, owner.String()) // best effort
			return err                                       // ctx cancelled -- shutdown
		}

		wg.Go(func() {
			defer p.PoolLimiter.ReleasePermit(ctx, owner.String())
			p.processClaim(ctx, item, consumerFunc)
		})
	}
}

// processClaim runs consumerFunc for one dispatched message and folds the outcome into p.buffer.
func (p *MessageConsumer[Message]) processClaim(ctx context.Context, item *Buffered, consumerFunc ConsumerFunc[Message]) {
	// sat in the queue too long to safely start -- surrendering the whole
	// range beats risking a lease overrun (another worker reclaiming the
	// same range while this message is still being worked).
	if item.lease.Until.Before(time.Now().Add(p.Config.WorkTimeout).Add(p.Config.AckMargin)) {
		p.buffer.MarkStale(item.lease.Token)
		return
	}

	var work Message
	if err := json.Unmarshal(item.row.Payload, &work); err != nil {
		// bad payload will never deserialize -- no point retrying it
		p.buffer.ResolveTerminal(item, err)
	} else if err := p.callSafely(withMeta(ctx, item.row.toMessageMeta()), consumerFunc, &work, item.row.Id, 0); err != nil {
		p.buffer.ResolveException(item, err)
	} else {
		p.buffer.ResolveSuccess(item)
	}

	// is the entire range full of messages resolve
	// ie was this item the final message to be processed in the range
	if !p.buffer.IsRangeResolved(item.lease.Token) {
		return
	}
	snapshot, err := p.buffer.TryGetRangeSnapshot(item.lease.Token)
	if err != nil {
		return // another resolver or shutdown owns the commit
	}
	p.commitRange(ctx, snapshot)
}

// commitRange finalizes a range once every message has resolved in it
func (p *MessageConsumer[Message]) commitRange(ctx context.Context, commit *rangeSnapshot) {
	// range always frees -- the lazy waterline roller advances committed
	// past it; failures ride along as parked exceptions, not a blocked range.
	err := p.Datastore.Commit(ctx, p.Topic.Id, p.consumerGroup, commit.Lease.Token, commit.Exceptions, commit.Terminals, p.Config.ExceptionInitialBackoff, p.Topic.DisableDeliveryLog)
	switch {
	case err == nil:
		p.buffer.Remove(commit.Lease.Token)
	case errors.Is(err, ErrLeaseLost):
		p.Logger.DebugContext(ctx, "lease lost at commit, ceded range to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "low", commit.Lease.Low, "high", commit.Lease.High)
		p.buffer.Remove(commit.Lease.Token) // reclaimed mid-range -- the new owner processes it, not a failure here
	default:
		// stays tracked -- CloseOpenRanges retries it on the way out
		p.Logger.WarnContext(ctx, "commit failed, range stays open for a retry at shutdown", "group", p.consumerGroup, "topic", p.Topic.Id, "low", commit.Lease.Low, "high", commit.Lease.High, "err", err)
	}
}

func (p *MessageConsumer[Message]) CursorPartialCommit(ctx context.Context, lastProcessed int64, lease LeaseRow, exceptions []MessageException, terminals []MessageTerminal) error {
	if lastProcessed == lease.Low && len(exceptions) == 0 && len(terminals) == 0 {
		return nil // interrupted before resolving anything -- leave the lease exactly as claimed
	}

	// the ctx that got us here is already Done -- the commit needs its own
	// bounded, uncancelled window to actually reach the DB, same as Shutdown
	commitCtx, cancel := context.WithTimeoutCause(context.WithoutCancel(ctx), p.Config.AckMargin,
		fmt.Errorf("partial commit exceeded AckMargin (%s) for group %q topic %d", p.Config.AckMargin, p.consumerGroup, p.Topic.Id))
	defer cancel()

	// narrow the lease to the untouched suffix instead of leaving the WHOLE
	// range (including the already-resolved prefix) to sit out a full reclaim.
	if err := p.Datastore.PartialCommit(commitCtx, p.Topic.Id, p.consumerGroup, lease.Token, lastProcessed, exceptions, terminals, p.Config.ExceptionInitialBackoff, p.Topic.DisableDeliveryLog); err != nil {
		if errors.Is(err, ErrLeaseLost) {
			p.Logger.DebugContext(ctx, "lease lost at partial commit, ceded range to new owner", "group", p.consumerGroup, "topic", p.Topic.Id, "low", lease.Low, "high", lease.High)
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
