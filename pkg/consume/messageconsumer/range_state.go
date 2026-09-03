package messageconsumer

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consume/messageconsumer/controller"
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
	kindSuperseded // dropped unrun -- its compacted message key has a newer version
	kindDeferred   // key busy at dispatch -- the commit writes its 'deferred' row
	kindDelayed    // the handler asked to run later -- the commit writes its 'ready' row at the delay
)

func (k outcomeKind) toOutcomeKind() (controller.OutcomeKind, bool) {
	switch k {
	case kindSuccess:
		return controller.OutcomeSuccess, true
	case kindException:
		return controller.OutcomeException, true
	case kindTerminal:
		return controller.OutcomeTerminal, true
	case kindSuperseded:
		return controller.OutcomeSuperseded, true
	case kindDeferred:
		return controller.OutcomeDeferred, true
	case kindDelayed:
		return controller.OutcomeDelayed, true
	}
	return "", false
}

// done gates kind/err via atomics release/acquire: kind/err are written
// FIRST, done SECOND, so Load()==true guarantees those writes are visible.
type result struct {
	done        atomic.Bool
	kind        outcomeKind
	err         string                   // empty for success
	delay       time.Duration            // kindDelayed only
	concurrency common.ConcurrencyPolicy // the policy resolved for the run
}

// zero value is the correct initial (pending) state -- resolve fills kind/err/done in later
func newResult() result {
	return result{}
}

// resolve writes kind/err THEN done -- done gates their visibility via
// atomics release/acquire, so the Store must come last.
func (r *result) resolve(kind outcomeKind, err string, delay time.Duration, concurrency common.ConcurrencyPolicy) {
	r.kind = kind
	r.err = err
	r.delay = delay
	r.concurrency = concurrency
	r.done.Store(true)
}

// a copy, not a live *rangeState -- holding it past a later Remove() can't race.
type rangeSnapshot struct {
	Lease    controller.RangeLease
	Outcomes []controller.MessageOutcome
}

func newRangeSnapshot(lease controller.RangeLease, outcomes []controller.MessageOutcome) *rangeSnapshot {
	return &rangeSnapshot{Lease: lease, Outcomes: outcomes}
}

// mutable bookkeeping for one claimed range, lives only inside
// claimBuffer.ranges. lock-free: results[i] is written by exactly one
// goroutine -- whichever one dequeued message i via waitForNext and later
// calls resolve* on it -- so no two goroutines ever touch the same memory.
type rangeState struct {
	lease controller.RangeLease
	ids   []int64  // message id per result index -- set once, read-only after
	keys  []string // message key per result index, "" if unset -- set once, read-only after
	total int

	// includeSuccesses adds success outcomes to the collected walks -- only
	// DeliveryLogModeAll wants them, so the common case skips the allocation
	includeSuccesses bool

	dispatched atomic.Int64 // count handed out by waitForNext
	resolved   atomic.Int64 // resolved==total means every result is done
	committed  atomic.Bool  // tryGetSnapshot's one-shot CAS
	stale      atomic.Bool
	results    []result
}

func newRangeState(claimed *controller.ClaimedRange, includeSuccesses bool) *rangeState {
	ids := make([]int64, len(claimed.Messages))
	keys := make([]string, len(claimed.Messages))
	results := make([]result, len(claimed.Messages))
	for i, claimedMessage := range claimed.Messages {
		ids[i] = claimedMessage.Id
		keys[i] = claimedMessage.MessageKey
		results[i] = newResult()
	}
	return &rangeState{
		lease:            claimed.Lease,
		ids:              ids,
		keys:             keys,
		total:            len(claimed.Messages),
		includeSuccesses: includeSuccesses,
		results:          results,
	}
}

func (r *rangeState) neverDispatched() bool {
	return r.dispatched.Load() == 0
}

func (r *rangeState) resolve(index int, kind outcomeKind, err string, delay time.Duration, concurrency common.ConcurrencyPolicy) {
	r.results[index].resolve(kind, err, delay, concurrency)
	r.resolved.Add(1)
}

// outcomeOf reads a result's kind; false while it is unresolved.
func (r *rangeState) outcomeOf(index int) (outcomeKind, bool) {
	current := &r.results[index]
	if !current.done.Load() {
		return 0, false
	}
	return current.kind, true
}

// isResolved returns true when all messages in range have been tracked / resolved.
func (r *rangeState) isResolved() bool {
	return r.resolved.Load() == int64(r.total)
}

// tryGetSnapshot hands the snapshot to exactly one caller: isResolved is
// checked BEFORE the CAS so a premature call can't burn snapshot ownership.
func (r *rangeState) tryGetSnapshot() (*rangeSnapshot, error) {
	if !r.isResolved() {
		return nil, errRangeNotResolved
	}
	if !r.committed.CompareAndSwap(false, true) {
		return nil, errSnapshotTaken
	}

	return newRangeSnapshot(r.lease, r.resolvedOutcomes()), nil
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
func (r *rangeState) contiguousResolved() (lastProcessed int64, outcomes []controller.MessageOutcome) {
	lastProcessed = r.lease.Low
	for i := range r.results {
		current := &r.results[i]
		if !current.done.Load() {
			break
		}
		lastProcessed = r.ids[i]
		if kind, ok := current.kind.toOutcomeKind(); ok && (r.includeSuccesses || kind != controller.OutcomeSuccess) {
			outcomes = append(outcomes, r.outcome(i, kind))
		}
	}
	return lastProcessed, outcomes
}

// same walk as contiguousResolved with no gap check -- only called once
// resolved==total, so every result is already guaranteed done.
func (r *rangeState) resolvedOutcomes() (outcomes []controller.MessageOutcome) {
	for i := range r.results {
		if kind, ok := r.results[i].kind.toOutcomeKind(); ok && (r.includeSuccesses || kind != controller.OutcomeSuccess) {
			outcomes = append(outcomes, r.outcome(i, kind))
		}
	}
	return outcomes
}

func (r *rangeState) outcome(index int, kind controller.OutcomeKind) controller.MessageOutcome {
	current := &r.results[index]
	return controller.MessageOutcome{
		MessageId:   r.ids[index],
		MessageKey:  r.keys[index],
		Concurrency: current.concurrency,
		Kind:        kind,
		Err:         current.err,
		Delay:       current.delay,
	}
}
