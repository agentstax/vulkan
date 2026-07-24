package consumer

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/jackc/pgx/v5/pgtype"
)

// inert queue entry -- no live pointers, so it stays valid even if its range
// gets removed while this message is still in flight.
type buffered struct {
	row   MessageRow
	lease LeaseRow // token used for staleness check, ForceReclaimRange
	index int      // this message's index in rangeState.results
}

func newBuffered(row MessageRow, lease LeaseRow, index int) *buffered {
	return &buffered{row: row, lease: lease, index: index}
}

// buffered are the dispatch unit, ranges the claim/commit unit.
// claimBuffer is the only thing that touches both, so they can't
// end up half-updated relative to each other.
type claimBuffer struct {
	queue concurrency.Queue[buffered]

	// guards `ranges` ONLY. RWMutex because lookup (read) fires once per
	// message while track/Remove (write) fire once per range -- reads
	// dominate writes by roughly BatchLimit:1.
	rangesMu sync.RWMutex
	ranges   map[pgtype.UUID]*rangeState
}

func NewClaimBuffer(queue concurrency.Queue[buffered]) (*claimBuffer, error) {
	if queue == nil {
		return nil, errors.New("queue must not be nil")
	}
	return &claimBuffer{
		queue:  queue,
		ranges: make(map[pgtype.UUID]*rangeState),
	}, nil
}

func (b *claimBuffer) WaitForRoom(ctx context.Context, timeout time.Duration, threshold int) (int, error) {
	return b.queue.WaitForRoom(ctx, timeout, threshold)
}

func (b *claimBuffer) Add(ctx context.Context, claimed *ClaimedRange) (*rangeCommit, error) {
	if claimed == nil {
		return nil, errors.New("claimed must not be nil")
	}

	state := newRangeState(claimed)
	if state.total == 0 {
		// nothing to dispatch, nothing to wait on -- this range skips b.track
		// below, so there's no CloseOpenRanges backstop to catch it later; the
		// non-nil commit is the only signal Prefetch gets to commit it now
		return newRangeCommit(state.lease, nil, nil), nil
	}

	// track BEFORE enqueueing: a mid-enqueue error still leaves the range
	// tracked, so CloseOpenRanges settles it instead of it leaking untracked
	b.track(state)
	for i, row := range claimed.Messages {
		item := newBuffered(row, claimed.Lease, i)
		if err := b.queue.EnQueue(ctx, item); err != nil {
			return nil, err
		}
	}
	return nil, nil
}

func (b *claimBuffer) track(state *rangeState) {
	b.rangesMu.Lock()
	defer b.rangesMu.Unlock()
	b.ranges[state.lease.Token] = state
}

func (b *claimBuffer) lookup(token pgtype.UUID) *rangeState {
	b.rangesMu.RLock()
	defer b.rangesMu.RUnlock()
	return b.ranges[token]
}

func (b *claimBuffer) WaitForNext(ctx context.Context) (*buffered, error) {
	item, err := b.queue.DeQueue(ctx) // DeQueue is blocking
	if err != nil {
		return nil, err
	}

	// plain counter for CloseOpenRanges' neverDispatched() check
	if state := b.lookup(item.lease.Token); state != nil {
		state.dispatched.Add(1)
	}
	return item, nil
}

func (b *claimBuffer) ResolveSuccess(item *buffered) *rangeCommit {
	return b.resolve(item, kindSuccess, "")
}

func (b *claimBuffer) ResolveException(item *buffered, err error) *rangeCommit {
	return b.resolve(item, kindException, err.Error())
}

func (b *claimBuffer) ResolveTerminal(item *buffered, err error) *rangeCommit {
	return b.resolve(item, kindTerminal, err.Error())
}

func (b *claimBuffer) resolve(item *buffered, kind outcomeKind, err string) *rangeCommit {
	state := b.lookup(item.lease.Token)
	if state == nil {
		return nil // already settled elsewhere -- fences a drain-timeout straggler
	}

	state.results[item.index].resolve(kind, err)

	if state.resolved.Add(1) != int64(state.total) {
		return nil // not the last message
	}

	// is the last message -> rangeState is resolved
	exceptions, terminals := state.resolvedExceptionsTerminals()
	return newRangeCommit(state.lease, exceptions, terminals)
}

func (b *claimBuffer) MarkStale(token pgtype.UUID) {
	if state := b.lookup(token); state != nil {
		state.stale.Store(true)
	}
}

func (b *claimBuffer) Remove(token pgtype.UUID) {
	b.rangesMu.Lock()
	defer b.rangesMu.Unlock()
	delete(b.ranges, token)
}

// empties the map atomically so shutdown owns every open range in one step --
// a straggler resolving after this hits the fence instead of racing it.
func (b *claimBuffer) RemoveAll() []*rangeState {
	b.rangesMu.Lock()
	defer b.rangesMu.Unlock()

	states := make([]*rangeState, 0, len(b.ranges))
	for _, state := range b.ranges {
		states = append(states, state)
	}
	clear(b.ranges)
	return states
}
