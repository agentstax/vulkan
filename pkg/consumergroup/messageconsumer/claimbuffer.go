package messageconsumer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
	"github.com/google/uuid"
)

var errRangeNotTracked = errors.New("range not tracked -- settled elsewhere")

// buffered is an inert queue entry -- no live pointers, so it stays valid even
// if its range gets removed while this message is still in flight.
type buffered struct {
	row   controller.Message
	lease controller.RangeLease // token used for staleness check, ForceReclaimRange
	index int                   // this message's index in rangeState.results
}

func newBuffered(row controller.Message, lease controller.RangeLease, index int) *buffered {
	return &buffered{row: row, lease: lease, index: index}
}

// buffered entries are the dispatch unit, ranges the claim/commit unit.
// claimBuffer is the only thing that touches both, so they can't
// end up half-updated relative to each other.
type claimBuffer struct {
	queue            concurrency.Queue[buffered]
	includeSuccesses bool // set on every rangeState this buffer tracks

	// guards `ranges` ONLY. RWMutex because lookup (read) fires once per
	// message while track/Remove (write) fire once per range -- reads
	// dominate writes by roughly BatchLimit:1.
	rangesMu sync.RWMutex
	ranges   map[uuid.UUID]*rangeState
}

func newClaimBuffer(queue concurrency.Queue[buffered], includeSuccesses bool) (*claimBuffer, error) {
	if queue == nil {
		return nil, errors.New("queue must not be nil")
	}
	return &claimBuffer{
		queue:            queue,
		includeSuccesses: includeSuccesses,
		ranges:           make(map[uuid.UUID]*rangeState),
	}, nil
}

func (b *claimBuffer) waitForRoom(ctx context.Context, timeout time.Duration, threshold int) (int, error) {
	return b.queue.WaitForRoom(ctx, timeout, threshold)
}

// add tracks claimed and enqueues its messages for dispatch.
func (b *claimBuffer) add(ctx context.Context, claimed *controller.ClaimedRange) error {
	if claimed == nil {
		return errors.New("claimed must not be nil")
	}
	if len(claimed.Messages) == 0 {
		return errors.New("claimed.Messages must not be empty")
	}

	state := newRangeState(claimed, b.includeSuccesses)

	// track BEFORE enqueueing: a mid-enqueue error still leaves the range
	// tracked, so closeOpenRanges settles it instead of it leaking untracked
	b.track(state)
	for i, row := range claimed.Messages {
		item := newBuffered(row, claimed.Lease, i)
		if err := b.queue.EnQueue(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (b *claimBuffer) track(state *rangeState) {
	b.rangesMu.Lock()
	defer b.rangesMu.Unlock()
	b.ranges[state.lease.Token] = state
}

func (b *claimBuffer) lookup(token uuid.UUID) *rangeState {
	b.rangesMu.RLock()
	defer b.rangesMu.RUnlock()
	return b.ranges[token]
}

func (b *claimBuffer) waitForNext(ctx context.Context) (*buffered, error) {
	item, err := b.queue.DeQueue(ctx) // DeQueue is blocking
	if err != nil {
		return nil, err
	}

	// plain counter for closeOpenRanges' neverDispatched() check
	if state := b.lookup(item.lease.Token); state != nil {
		state.dispatched.Add(1)
	}
	return item, nil
}

func (b *claimBuffer) resolveSuccess(item *buffered) {
	b.resolve(item, kindSuccess, "", 0)
}

func (b *claimBuffer) resolveException(item *buffered, err error) {
	b.resolve(item, kindException, err.Error(), 0)
}

func (b *claimBuffer) resolveTerminal(item *buffered, err error) {
	b.resolve(item, kindTerminal, err.Error(), 0)
}

func (b *claimBuffer) resolveSuperseded(item *buffered) {
	b.resolve(item, kindSuperseded, "a newer version of the same message key superseded this delivery", 0)
}

func (b *claimBuffer) resolveDeferred(item *buffered) {
	b.resolve(item, kindDeferred, "another delivery held the message key at dispatch", 0)
}

func (b *claimBuffer) resolveDelayed(item *buffered, delayed *consumergroup.DelayedDelivery) {
	b.resolve(item, kindDelayed, delayed.Error(), delayed.Delay)
}

func (b *claimBuffer) resolve(item *buffered, kind outcomeKind, err string, delay time.Duration) {
	state := b.lookup(item.lease.Token)
	if state == nil {
		return // already settled elsewhere -- fences a drain-timeout straggler
	}
	state.resolve(item.index, kind, err, delay)
}

func (b *claimBuffer) isRangeResolved(token uuid.UUID) bool {
	state := b.lookup(token)
	return state != nil && state.isResolved()
}

// errors are normal flow -- several resolvers can see IsRangeResolved true,
// only tryGetSnapshot's CompareAndSwap winner gets the snapshot.
func (b *claimBuffer) tryGetRangeSnapshot(token uuid.UUID) (*rangeSnapshot, error) {
	state := b.lookup(token)
	if state == nil {
		return nil, errRangeNotTracked
	}
	return state.tryGetSnapshot()
}

func (b *claimBuffer) markStale(token uuid.UUID) {
	if state := b.lookup(token); state != nil {
		state.stale.Store(true)
	}
}

func (b *claimBuffer) remove(token uuid.UUID) {
	b.rangesMu.Lock()
	defer b.rangesMu.Unlock()
	delete(b.ranges, token)
}

// empties the map atomically so shutdown owns every open range in one step --
// a straggler resolving after this hits the fence instead of racing it.
func (b *claimBuffer) removeAll() []*rangeState {
	b.rangesMu.Lock()
	defer b.rangesMu.Unlock()

	states := make([]*rangeState, 0, len(b.ranges))
	for _, state := range b.ranges {
		states = append(states, state)
	}
	clear(b.ranges)
	return states
}
