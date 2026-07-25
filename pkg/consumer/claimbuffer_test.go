package consumer

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/jackc/pgx/v5/pgtype"
)

func testToken(b byte) pgtype.UUID {
	return pgtype.UUID{Bytes: [16]byte{0: b}, Valid: true}
}

func testRange(token pgtype.UUID, ids ...int64) *ClaimedRange {
	messages := make([]MessageRow, len(ids))
	for i, id := range ids {
		messages[i] = MessageRow{Id: id}
	}
	low := int64(0)
	if len(ids) > 0 {
		low = ids[0] - 1
	}
	return &ClaimedRange{
		Lease:    LeaseRow{Token: token, Low: low, High: low + int64(len(ids))},
		Messages: messages,
	}
}

// pokes a slot directly, mirroring what claimBuffer.resolve writes -- used to
// set up rangeState fixtures without going through WaitForNext/Resolve*.
func resolveSlot(state *rangeState, index int, kind outcomeKind, err string) {
	state.results[index].kind = kind
	state.results[index].err = err
	state.results[index].done.Store(true)
}

func newTestBuffer(t *testing.T, cap int) *claimBuffer {
	t.Helper()
	queue, err := concurrency.NewPressureQueue[Buffered](cap)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	buf, err := NewClaimBuffer(queue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	return buf
}

func TestNewClaimBuffer_RequiresQueue(t *testing.T) {
	if _, err := NewClaimBuffer(nil); err == nil {
		t.Fatal("expected error for nil queue")
	}
}

func TestClaimBuffer_AddTracksBeforeEnqueue(t *testing.T) {
	buf := newTestBuffer(t, 3)
	rng := testRange(testToken(1), 1, 2, 3)

	if err := buf.Add(context.Background(), rng); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, tracked := buf.ranges[rng.Lease.Token]; !tracked {
		t.Fatal("range should be tracked after Add")
	}
}

// a mid-enqueue error (ctx cancelled while EnQueue blocks) must leave the
// range tracked -- CloseOpenRanges is the backstop that settles it later,
// so losing the entry here would leak the claimed lease forever.
func TestClaimBuffer_AddMidEnqueueErrorLeavesRangeTracked(t *testing.T) {
	buf := newTestBuffer(t, 1) // cap 1: first EnQueue succeeds, second blocks
	rng := testRange(testToken(2), 10, 11, 12)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err := buf.Add(ctx, rng)
	if err == nil {
		t.Fatal("expected an error from the blocked EnQueue")
	}
	if _, tracked := buf.ranges[rng.Lease.Token]; !tracked {
		t.Fatal("range must remain tracked after a mid-enqueue error")
	}
}

// an empty range (every message compacted away) has nothing to dispatch or
// resolve -- Add rejects it, the caller (prefetch) is the one that commits
// it directly instead of tracking a range no Resolve call could ever settle.
func TestClaimBuffer_AddRejectsEmptyRange(t *testing.T) {
	buf := newTestBuffer(t, 3)
	rng := testRange(testToken(3)) // zero messages

	if err := buf.Add(context.Background(), rng); err == nil {
		t.Fatal("expected an error for an empty range")
	}
	if _, tracked := buf.ranges[rng.Lease.Token]; tracked {
		t.Fatal("an empty range must not be tracked")
	}
}

func TestClaimBuffer_WaitForNextAdvancesDispatched(t *testing.T) {
	buf := newTestBuffer(t, 3)
	rng := testRange(testToken(4), 1, 2, 3)
	if err := buf.Add(context.Background(), rng); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i := range 3 {
		item, err := buf.WaitForNext(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if item.index != i {
			t.Fatalf("dequeue order = index %d, want %d (FIFO)", item.index, i)
		}
	}

	state := buf.ranges[rng.Lease.Token]
	if got := state.dispatched.Load(); got != 3 {
		t.Fatalf("dispatched = %d, want 3", got)
	}
}

// concurrent WaitForNext callers must never lose or double-count a dispatch --
// run under -race to also prove no data race on the shared counter.
func TestClaimBuffer_WaitForNextConcurrentDispatchCount(t *testing.T) {
	const n = 200
	buf := newTestBuffer(t, n)
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	rng := testRange(testToken(5), ids...)
	if err := buf.Add(context.Background(), rng); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := buf.WaitForNext(context.Background()); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	state := buf.ranges[rng.Lease.Token]
	if got := state.dispatched.Load(); got != n {
		t.Fatalf("dispatched = %d, want %d", got, n)
	}
}

// exactly one of N concurrent resolve-then-TryGetRangeSnapshot chains for a
// range's N distinct slots must win the commit snapshot -- run under -race.
func TestClaimBuffer_ResolveLastResolverCommitsExactlyOnce(t *testing.T) {
	const n = 100
	buf := newTestBuffer(t, n)
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	rng := testRange(testToken(6), ids...)
	if err := buf.Add(context.Background(), rng); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	items := make([]*Buffered, n)
	for i := range n {
		item, err := buf.WaitForNext(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items[i] = item
	}

	var commits atomic.Int64
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(item *Buffered) {
			defer wg.Done()
			buf.ResolveSuccess(item)
			if !buf.IsRangeResolved(item.lease.Token) {
				return
			}
			if _, err := buf.TryGetRangeSnapshot(item.lease.Token); err == nil {
				commits.Add(1)
			}
		}(items[i])
	}
	wg.Wait()

	if got := commits.Load(); got != 1 {
		t.Fatalf("commits handed out = %d, want exactly 1", got)
	}
}

// disjoint-index concurrent Resolves must never race (run under -race) and
// must all be reflected in the final commit's exceptions/terminals.
func TestClaimBuffer_ResolveSlotExclusivity(t *testing.T) {
	const n = 60
	buf := newTestBuffer(t, n)
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = int64(i + 1)
	}
	rng := testRange(testToken(7), ids...)
	if err := buf.Add(context.Background(), rng); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	items := make([]*Buffered, n)
	for i := range n {
		item, err := buf.WaitForNext(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		items[i] = item
	}

	var finalCommit atomic.Pointer[rangeSnapshot]
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(item *Buffered) {
			defer wg.Done()
			switch item.index % 3 {
			case 0:
				buf.ResolveSuccess(item)
			case 1:
				buf.ResolveException(item, errors.New("retryable"))
			case 2:
				buf.ResolveTerminal(item, errors.New("bad payload"))
			}
			if !buf.IsRangeResolved(item.lease.Token) {
				return
			}
			if commit, err := buf.TryGetRangeSnapshot(item.lease.Token); err == nil {
				finalCommit.Store(commit)
			}
		}(items[i])
	}
	wg.Wait()

	commit := finalCommit.Load()
	if commit == nil {
		t.Fatal("expected a commit once every slot resolved")
	}
	wantExceptions, wantTerminals := 0, 0
	for i := range n {
		switch i % 3 {
		case 1:
			wantExceptions++
		case 2:
			wantTerminals++
		}
	}
	if len(commit.Exceptions) != wantExceptions {
		t.Fatalf("exceptions = %d, want %d", len(commit.Exceptions), wantExceptions)
	}
	if len(commit.Terminals) != wantTerminals {
		t.Fatalf("terminals = %d, want %d", len(commit.Terminals), wantTerminals)
	}
}

// a resolve for a range that's already been removed (settled by
// CloseOpenRanges or an earlier commit) must no-op instead of mutating or
// double-committing work someone else already closed out.
func TestClaimBuffer_ResolveAfterRemoveIsFenced(t *testing.T) {
	buf := newTestBuffer(t, 1)
	rng := testRange(testToken(8), 1)
	if err := buf.Add(context.Background(), rng); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item, err := buf.WaitForNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf.Remove(rng.Lease.Token)

	buf.ResolveSuccess(item) // fenced no-op
	if buf.IsRangeResolved(item.lease.Token) {
		t.Fatal("expected IsRangeResolved false after Remove")
	}
	if _, err := buf.TryGetRangeSnapshot(item.lease.Token); !errors.Is(err, errRangeNotTracked) {
		t.Fatalf("err = %v, want errRangeNotTracked after Remove", err)
	}
}

func TestClaimBuffer_ResolveAfterRemoveAllIsFenced(t *testing.T) {
	buf := newTestBuffer(t, 1)
	rng := testRange(testToken(9), 1)
	if err := buf.Add(context.Background(), rng); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	item, err := buf.WaitForNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	removed := buf.RemoveAll()
	if len(removed) != 1 {
		t.Fatalf("RemoveAll returned %d ranges, want 1", len(removed))
	}

	buf.ResolveSuccess(item) // fenced no-op
	if buf.IsRangeResolved(item.lease.Token) {
		t.Fatal("expected IsRangeResolved false after RemoveAll")
	}
	if _, err := buf.TryGetRangeSnapshot(item.lease.Token); !errors.Is(err, errRangeNotTracked) {
		t.Fatalf("err = %v, want errRangeNotTracked after RemoveAll", err)
	}
}

// a premature TryGetRangeSnapshot (range not fully resolved) must error
// WITHOUT consuming commit ownership -- the real final resolver still gets
// the snapshot afterward.
func TestClaimBuffer_TryGetRangeSnapshotBeforeResolvedDoesNotBurnCommit(t *testing.T) {
	buf := newTestBuffer(t, 2)
	rng := testRange(testToken(13), 1, 2)
	if err := buf.Add(context.Background(), rng); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	first, err := buf.WaitForNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	second, err := buf.WaitForNext(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	buf.ResolveSuccess(first)
	if _, err := buf.TryGetRangeSnapshot(rng.Lease.Token); !errors.Is(err, errRangeNotResolved) {
		t.Fatalf("err = %v, want errRangeNotResolved for a half-resolved range", err)
	}

	buf.ResolveSuccess(second)
	snapshot, err := buf.TryGetRangeSnapshot(rng.Lease.Token)
	if err != nil || snapshot == nil {
		t.Fatalf("the final resolver must still get the snapshot, got (%v, %v)", snapshot, err)
	}
}

func TestClaimBuffer_MarkStaleNoOpOnUnknownToken(t *testing.T) {
	buf := newTestBuffer(t, 1)
	buf.MarkStale(testToken(99)) // must not panic
}

// RemoveAll must hand back every currently-tracked range exactly once and
// leave the buffer empty for any later caller.
func TestClaimBuffer_RemoveAllExactlyOnce(t *testing.T) {
	buf := newTestBuffer(t, 10)
	tokens := []pgtype.UUID{testToken(10), testToken(11), testToken(12)}
	for i, tok := range tokens {
		rng := testRange(tok, int64(i)+1)
		if err := buf.Add(context.Background(), rng); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	first := buf.RemoveAll()
	if len(first) != len(tokens) {
		t.Fatalf("first RemoveAll returned %d, want %d", len(first), len(tokens))
	}

	second := buf.RemoveAll()
	if len(second) != 0 {
		t.Fatalf("second RemoveAll returned %d, want 0 (already drained)", len(second))
	}
}

// the safety property the whole partial-commit design rests on: a message
// that resolved past a gap (an earlier index still not done) must NOT be
// reported as part of the contiguous prefix -- it rides the untouched
// suffix back to reclaim instead of the reclaim silently skipping over the
// still-unresolved entry before it.
func TestRangeState_ContiguousResolvedStopsAtGap(t *testing.T) {
	rng := testRange(testToken(13), 101, 102, 103, 104, 105)
	state := newRangeState(rng)

	// resolve everything except index 1 (id 102) -- a gap in the middle
	resolveSlot(state, 0, kindSuccess, "")
	resolveSlot(state, 2, kindException, "retryable")
	resolveSlot(state, 3, kindTerminal, "bad payload")
	resolveSlot(state, 4, kindSuccess, "")

	lastProcessed, exceptions, terminals := state.contiguousResolved()
	if lastProcessed != 101 {
		t.Fatalf("lastProcessed = %d, want 101 (stop before the gap at 102)", lastProcessed)
	}
	if len(exceptions) != 0 {
		t.Fatalf("exceptions past the gap must be excluded, got %d", len(exceptions))
	}
	if len(terminals) != 0 {
		t.Fatalf("terminals past the gap must be excluded, got %d", len(terminals))
	}
}

func TestRangeState_ContiguousResolvedNoGap(t *testing.T) {
	rng := testRange(testToken(14), 201, 202, 203)
	state := newRangeState(rng)

	resolveSlot(state, 0, kindSuccess, "")
	resolveSlot(state, 1, kindException, "retryable")
	resolveSlot(state, 2, kindTerminal, "bad payload")

	lastProcessed, exceptions, terminals := state.contiguousResolved()
	if lastProcessed != 203 {
		t.Fatalf("lastProcessed = %d, want 203", lastProcessed)
	}
	if len(exceptions) != 1 || len(terminals) != 1 {
		t.Fatalf("exceptions=%d terminals=%d, want 1 and 1", len(exceptions), len(terminals))
	}
}

func TestRangeState_ContiguousResolvedNothingDone(t *testing.T) {
	rng := testRange(testToken(15), 301, 302)
	state := newRangeState(rng)

	lastProcessed, exceptions, terminals := state.contiguousResolved()
	if lastProcessed != rng.Lease.Low {
		t.Fatalf("lastProcessed = %d, want %d (untouched -> stays at Low)", lastProcessed, rng.Lease.Low)
	}
	if exceptions != nil || terminals != nil {
		t.Fatal("expected no exceptions/terminals when nothing resolved")
	}
}

func TestRangeState_NeverDispatched(t *testing.T) {
	rng := testRange(testToken(16), 1, 2)
	state := newRangeState(rng)
	if !state.neverDispatched() {
		t.Fatal("a fresh range must report never dispatched")
	}
	state.dispatched.Add(1)
	if state.neverDispatched() {
		t.Fatal("expected neverDispatched to flip false after a dispatch")
	}
}
