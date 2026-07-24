package consumer

import "sync/atomic"

type outcomeKind int

const (
	kindSuccess outcomeKind = iota
	kindException
	kindTerminal
)

// done gates kind/err via atomics release/acquire: kind/err are written
// FIRST, done SECOND, so Load()==true guarantees those writes are visible.
type slot struct {
	done atomic.Bool
	kind outcomeKind
	err  string // empty for success
}

// zero value is the correct initial (pending) state -- resolve fills kind/err/done in later
func newSlot() slot {
	return slot{}
}

// resolve writes kind/err THEN done -- done gates their visibility via
// atomics release/acquire, so the Store must come last.
func (s *slot) resolve(kind outcomeKind, err string) {
	s.kind = kind
	s.err = err
	s.done.Store(true)
}

// Snapshot Add/Resolve hand back when a range is ready to commit -- a copy,
// not a live *rangeState, so holding it past a later Remove() can't race.
type rangeCommit struct {
	Lease      LeaseRow
	Exceptions []MessageException
	Terminals  []MessageTerminal
}

func newRangeCommit(lease LeaseRow, exceptions []MessageException, terminals []MessageTerminal) *rangeCommit {
	return &rangeCommit{Lease: lease, Exceptions: exceptions, Terminals: terminals}
}

// mutable bookkeeping for one claimed range, lives only inside
// claimBuffer.ranges. lock-free: results[i] is written by exactly one
// goroutine -- whichever one dequeued message i via WaitForNext and later
// calls Resolve* on it -- so no two goroutines ever touch the same memory.
type rangeState struct {
	lease LeaseRow
	ids   []int64 // message id per slot index -- set once, read-only after
	total int

	dispatched atomic.Int64 // count handed out by WaitForNext
	resolved   atomic.Int64 // resolved.Add(1)'s return value is unique per caller -- whoever sees it hit total is the sole committer
	stale      atomic.Bool
	results    []slot
}

func newRangeState(claimed *ClaimedRange) *rangeState {
	ids := make([]int64, len(claimed.Messages))
	results := make([]slot, len(claimed.Messages))
	for i, m := range claimed.Messages {
		ids[i] = m.Id
		results[i] = newSlot()
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

// traverse results from index 0, stopping at the first unresolved slot, so the
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
func (r *rangeState) contiguousResolved() (lastProcessed int64, exceptions []MessageException, terminals []MessageTerminal) {
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
		}
	}
	return lastProcessed, exceptions, terminals
}

// same walk as contiguousResolved with no gap check -- only called once
// resolved==total, so every slot is already guaranteed done.
func (r *rangeState) resolvedExceptionsTerminals() (exceptions []MessageException, terminals []MessageTerminal) {
	for i := range r.results {
		switch r.results[i].kind {
		case kindException:
			exceptions = append(exceptions, MessageException{MessageId: r.ids[i], Err: r.results[i].err})
		case kindTerminal:
			terminals = append(terminals, MessageTerminal{MessageId: r.ids[i], Err: r.results[i].err})
		}
	}
	return exceptions, terminals
}
