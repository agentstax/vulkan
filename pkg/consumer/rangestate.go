package consumer

import (
	"errors"
	"sync/atomic"
)

var (
	errRangeNotResolved = errors.New("range not fully resolved")
	errSnapshotTaken    = errors.New("snapshot already taken by another resolver")
)

type outcomeKind int

const (
	kindSuccess outcomeKind = iota
	kindException
	kindTerminal
	kindSuperseded // dropped unrun -- a newer message on its compaction key exists
	kindDeferred   // key busy at dispatch -- the commit writes its 'deferred' row
)

// done gates kind/err via atomics release/acquire: kind/err are written
// FIRST, done SECOND, so Load()==true guarantees those writes are visible.
type result struct {
	done atomic.Bool
	kind outcomeKind
	err  string // empty for success
}

// zero value is the correct initial (pending) state -- resolve fills kind/err/done in later
func newResult() result {
	return result{}
}

// resolve writes kind/err THEN done -- done gates their visibility via
// atomics release/acquire, so the Store must come last.
func (s *result) resolve(kind outcomeKind, err string) {
	s.kind = kind
	s.err = err
	s.done.Store(true)
}

// a copy, not a live *rangeState -- holding it past a later Remove() can't race.
type rangeSnapshot struct {
	Lease      LeaseRow
	Exceptions []MessageException
	Terminals  []MessageTerminal
	Superseded []MessageSuperseded
	Deferred   []MessageDeferred
}

func newRangeSnapshot(lease LeaseRow, exceptions []MessageException, terminals []MessageTerminal, superseded []MessageSuperseded, deferred []MessageDeferred) *rangeSnapshot {
	return &rangeSnapshot{Lease: lease, Exceptions: exceptions, Terminals: terminals, Superseded: superseded, Deferred: deferred}
}

// mutable bookkeeping for one claimed range, lives only inside
// claimBuffer.ranges. lock-free: results[i] is written by exactly one
// goroutine -- whichever one dequeued message i via WaitForNext and later
// calls Resolve* on it -- so no two goroutines ever touch the same memory.
type rangeState struct {
	lease LeaseRow
	ids   []int64 // message id per result index -- set once, read-only after
	total int

	dispatched atomic.Int64 // count handed out by WaitForNext
	resolved   atomic.Int64 // resolved==total means every result is done
	committed  atomic.Bool  // TryGetSnapshot's one-shot CAS
	stale      atomic.Bool
	results    []result
}

func newRangeState(claimed *ClaimedRange) *rangeState {
	ids := make([]int64, len(claimed.Messages))
	results := make([]result, len(claimed.Messages))
	for i, m := range claimed.Messages {
		ids[i] = m.Id
		results[i] = newResult()
	}
	return &rangeState{
		lease:   claimed.Lease,
		ids:     ids,
		total:   len(claimed.Messages),
		results: results,
	}
}

func (r *rangeState) neverDispatched() bool {
	return r.dispatched.Load() == 0
}

func (r *rangeState) resolve(index int, kind outcomeKind, err string) {
	r.results[index].resolve(kind, err)
	r.resolved.Add(1)
}

// isResolved returns true when all messages in range have been tracked / resolved.
func (r *rangeState) isResolved() bool {
	return r.resolved.Load() == int64(r.total)
}

// TryGetSnapshot hands the snapshot to exactly one caller: isResolved is
// checked BEFORE the CAS so a premature call can't burn snapshot ownership.
func (r *rangeState) TryGetSnapshot() (*rangeSnapshot, error) {
	if !r.isResolved() {
		return nil, errRangeNotResolved
	}
	if !r.committed.CompareAndSwap(false, true) {
		return nil, errSnapshotTaken
	}

	exceptions, terminals, superseded, deferred := r.resolvedOutcomes()
	return newRangeSnapshot(r.lease, exceptions, terminals, superseded, deferred), nil
}

// traverse results from index 0, stopping at the first unresolved result, so the
// commit only ever advances the watermark over a CONTIGUOUS run of resolved
// messages -- PartialCommit can't skip past work that's still in flight.
//
//	index:  0     1     2     3     4
//	id:     100   101   102   103   104
//	state:  done  PEND  done  done  done
//	                ^ stops here
//
// lastProcessed stays 100 -- 102-104 are dropped from this commit even
// though they're resolved. They stay leased alongside 101 and all three
// get redelivered together on expiry.
func (r *rangeState) contiguousResolved() (lastProcessed int64, exceptions []MessageException, terminals []MessageTerminal, superseded []MessageSuperseded, deferred []MessageDeferred) {
	lastProcessed = r.lease.Low
	for i := range r.results {
		s := &r.results[i]
		if !s.done.Load() {
			break
		}
		lastProcessed = r.ids[i]
		switch s.kind {
		case kindException:
			exceptions = append(exceptions, MessageException{MessageId: r.ids[i], Err: s.err})
		case kindTerminal:
			terminals = append(terminals, MessageTerminal{MessageId: r.ids[i], Err: s.err})
		case kindSuperseded:
			superseded = append(superseded, MessageSuperseded{MessageId: r.ids[i], Err: s.err})
		case kindDeferred:
			deferred = append(deferred, MessageDeferred{MessageId: r.ids[i], Err: s.err})
		}
	}
	return lastProcessed, exceptions, terminals, superseded, deferred
}

// same walk as contiguousResolved with no gap check -- only called once
// resolved==total, so every result is already guaranteed done.
func (r *rangeState) resolvedOutcomes() (exceptions []MessageException, terminals []MessageTerminal, superseded []MessageSuperseded, deferred []MessageDeferred) {
	for i := range r.results {
		switch r.results[i].kind {
		case kindException:
			exceptions = append(exceptions, MessageException{MessageId: r.ids[i], Err: r.results[i].err})
		case kindTerminal:
			terminals = append(terminals, MessageTerminal{MessageId: r.ids[i], Err: r.results[i].err})
		case kindSuperseded:
			superseded = append(superseded, MessageSuperseded{MessageId: r.ids[i], Err: r.results[i].err})
		case kindDeferred:
			deferred = append(deferred, MessageDeferred{MessageId: r.ids[i], Err: r.results[i].err})
		}
	}
	return exceptions, terminals, superseded, deferred
}
