package main

// Phase 10 lab: drive each failure mode the harness flags (--fail-rate,
// --sleep, --crash-after) represent through the real MessageConsumer/Datastore
// paths, and assert the metrics snapshot moves EXACTLY the number(s) that
// failure mode should move -- LEARNING_PLAN.md's own check: "if a failure
// doesn't move a number, you have a blind spot."
//
// Four scenarios, run in sequence against one topic (ids 1-3, 4-6, 7-9):
//  1. retryable failure (--fail-rate)     -> ready exception
//  2. sustained failure / exhausted retries -> ready exception dead-letters
//  3. hard timeout (--sleep past WorkTimeout) -> abandoned goroutine, then
//     ready exception; goroutine self-clears
//  4. crash mid-range (--crash-after, never Commit) -> orphaned lease, then
//     reclaimed once it expires
//
// Each scenario snapshots before/after and diffs every tracked number, not
// just the one it expects to move -- a nonzero diff anywhere else is exactly
// the "blind spot" (or an unwanted side effect) this lab exists to catch.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const (
	group = "phase10.metricsreactionlab"
	batch = 3
	lease = 3 * time.Second
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx))

	topicName := fmt.Sprintf("%s.%d", group, time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topic.Config{})
	must(err)
	defer func() { must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true})) }()

	wp, err := producer.NewProducer[common.Work](tp.Name, topic.SchemaVersion(1), ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	queue, err := concurrency.NewPressureQueue[consumer.Buffered](30)
	must(err)
	// N=1 -- scenario 1/2 depend on failingId1 being the SPECIFIC message that
	// fails; with true concurrent dispatch (N>1), which message wins the race
	// to be call #1 isn't deterministic. This lab tests metric reactions to
	// failure modes, not concurrency itself.
	pool, err := concurrency.NewWorkerPoolLimiter(1)
	must(err)

	wc, err := consumer.NewMessageConsumer[common.Work](group, tp.Name, topic.SchemaVersion(1), queue, pool, ds, &consumer.ConsumerConfig{
		DisableGracefulShutdown: true,
		BatchLimit:              batch,
		WorkTimeout:             1 * time.Second,
		WorkTimeoutGrace:        100 * time.Millisecond,
		ExceptionInitialBackoff: 300 * time.Millisecond,
	})
	must(err)
	must(wc.Register(ctx))

	// seeded one range at a time, right before the scenario that consumes it --
	// Process claims continuously once anything is available, so pre-seeding
	// all 9 up front would let scenario 1 race ahead and claim scenarios 3 and
	// 4's ranges too before this lab ever gets to run them.
	seed(ctx, wp, 3) // ids 1-3
	scenarioFailRate(ctx, wc)
	scenarioExhaustedRetries(ctx, wc)
	seed(ctx, wp, 3) // ids 4-6
	scenarioHardTimeout(ctx, wc)
	seed(ctx, wp, 3) // ids 7-9
	scenarioCrash(ctx, wc)

	fmt.Println("\n✅ METRICS REACTION LAB PASSED")
	fmt.Println("   every induced failure mode moved exactly the snapshot number(s) it should have -- no blind spots.")
}

// ---- scenario 1: retryable failure (--fail-rate) ----

const failingId1 = int64(1)

func scenarioFailRate(ctx context.Context, wc *consumer.MessageConsumer[common.Work]) {
	step("SCENARIO 1: retryable failure (--fail-rate equivalent) -> ready exception")
	before := snapshotCounts(ctx, wc)

	var calls atomic.Int64
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		if calls.Add(1) == 1 {
			return errors.New("artificial failure from -fail-rate")
		}
		return nil
	}
	runProcessUntil(ctx, wc, consumerFunc, 5*time.Second, func() bool {
		return calls.Load() == 3 && openLeases(ctx, wc) == 0
	})
	assert("all 3 messages in range 1 attempted", calls.Load(), 3)

	after := snapshotCounts(ctx, wc)
	assertDelta("fail-rate failure parks exactly one ready exception", before, after, counts{Ready: 1})
}

// ---- scenario 2: sustained failure exhausts retries -> dead-letter ----

func scenarioExhaustedRetries(ctx context.Context, wc *consumer.MessageConsumer[common.Work]) {
	step("SCENARIO 2: sustained --fail-rate exhausts retries -> ready exception dead-letters")
	before := snapshotCounts(ctx, wc)

	step("sleep past ExceptionInitialBackoff so message 1's parked exception is claimable")
	time.Sleep(600 * time.Millisecond)

	// maxAttempts=1 -- the claim's own Attempts (already >=1 from scenario 1's
	// park) immediately satisfies it, so this dead-letters on the first retry
	// instead of requiring a real multi-attempt backoff sequence.
	claimed, err := wc.Datastore.ClaimExceptions(ctx, wc.Topic.Id, group, batch, 1, lease, false)
	must(err)
	if len(claimed) != 1 || claimed[0].MessageId != failingId1 {
		die(fmt.Sprintf("expected to claim exactly message %d, got %+v", failingId1, claimed))
	}

	// while claimed for retry (leased out, not yet resolved) the exception sits
	// in 'inflight', not 'ready' -- the one transition the other 3 scenarios
	// never exercise, so it gets its own explicit checkpoint here.
	mid := snapshotCounts(ctx, wc)
	assertDelta("claiming the exception for retry moves it from ready to inflight", before, mid, counts{Ready: -1, Inflight: 1})

	must(wc.Datastore.RecordExceptionFailure(ctx, 1, &claimed[0], errors.New("retries exhausted"), false))

	after := snapshotCounts(ctx, wc)
	assertDelta("exhausted retries move the exception from inflight to dead", mid, after, counts{Inflight: -1, Dead: 1})
}

// ---- scenario 3: hard timeout / hang (--sleep past WorkTimeout) ----

func scenarioHardTimeout(ctx context.Context, wc *consumer.MessageConsumer[common.Work]) {
	step("SCENARIO 3: hard timeout (--sleep past WorkTimeout) -> abandoned goroutine, then ready exception")
	before := snapshotCounts(ctx, wc)

	const hangFor = 2 * time.Second // outlives WorkTimeout(1s)+Grace(100ms)
	var calls atomic.Int64
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		if calls.Add(1) == 1 {
			time.Sleep(hangFor)
		}
		return nil
	}
	elapsed := runProcessUntil(ctx, wc, consumerFunc, 5*time.Second, func() bool {
		return calls.Load() == 3 && openLeases(ctx, wc) == 0
	})
	if elapsed >= hangFor {
		die(fmt.Sprintf("range took %s to commit -- it should have abandoned message 4 around WorkTimeout+Grace, not waited out the full %s hang", elapsed, hangFor))
	}

	mid := snapshotCounts(ctx, wc)
	assertDelta("hang abandons exactly one goroutine and parks a ready exception, without waiting it out",
		before, mid, counts{Ready: 1, AbandonedOutstanding: 1, AbandonedTotal: 1})

	step("waiting for the abandoned goroutine to finish its sleep and self-clear")
	deadline := time.Now().Add(5 * time.Second)
	for wc.Metrics.AbandonedRoutines.Snapshot().Outstanding > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}

	after := snapshotCounts(ctx, wc)
	assertDelta("abandoned goroutine self-clears -- outstanding drops back, everything else holds", mid, after, counts{AbandonedOutstanding: -1})
}

// ---- scenario 4: crash mid-range (--crash-after, never Commit) ----

func scenarioCrash(ctx context.Context, wc *consumer.MessageConsumer[common.Work]) {
	step("SCENARIO 4: crash mid-range (--crash-after equivalent, never Commit) -> orphaned lease")
	before := snapshotCounts(ctx, wc)

	claim, err := wc.Datastore.ClaimMessagesWithCursor(ctx, wc.Topic.Id, group, batch, 3, lease, false)
	must(err)
	if claim == nil {
		die("expected a fresh claim for scenario 4, got nil")
	}
	// *** CRASH: control never reaches Commit(claim) ***

	mid := snapshotCounts(ctx, wc)
	assertDelta("a claimed-but-never-committed range leaves exactly one open lease", before, mid, counts{OpenLeases: 1})

	step(fmt.Sprintf("sleep %s -- let the crashed lease expire", lease+500*time.Millisecond))
	time.Sleep(lease + 500*time.Millisecond)

	reclaim, err := wc.Datastore.ClaimMessagesWithCursor(ctx, wc.Topic.Id, group, batch, 3, lease, false)
	must(err)
	if reclaim == nil {
		die("expected a reclaim, got nil")
	}
	must(wc.Datastore.Commit(ctx, wc.Topic.Id, group, reclaim.Lease.Token, nil, nil, 300*time.Millisecond, false))

	after := snapshotCounts(ctx, wc)
	assertDelta("reclaim + commit releases the orphaned lease", mid, after, counts{OpenLeases: -1})
}

// ---- snapshot diffing ----

type counts struct {
	Ready, Inflight, Dead, OpenLeases, AbandonedOutstanding, AbandonedTotal int64
}

// openLeases is snapshotCounts' OpenLeases field alone, without the
// print-every-call side effect -- safe to poll in a tight loop.
func openLeases(ctx context.Context, wc *consumer.MessageConsumer[common.Work]) int64 {
	snap, err := wc.Metrics.Snapshot(ctx)
	must(err)
	return snap.QueueState.OpenLeases
}

func snapshotCounts(ctx context.Context, wc *consumer.MessageConsumer[common.Work]) counts {
	snap, err := wc.Metrics.Snapshot(ctx)
	must(err)
	fmt.Println(snap.String())
	return counts{
		Ready:                snap.QueueState.ReadyExceptions,
		Inflight:             snap.QueueState.InflightExceptions,
		Dead:                 snap.QueueState.DeadExceptions,
		OpenLeases:           snap.QueueState.OpenLeases,
		AbandonedOutstanding: int64(snap.AbandonedRoutines.Outstanding),
		AbandonedTotal:       int64(snap.AbandonedRoutines.Total),
	}
}

// assertDelta checks that exactly the fields named in want changed between
// before/after by the given amounts, and every other tracked field is
// unchanged -- this is LEARNING_PLAN.md's "if a failure doesn't move a
// number, you have a blind spot" check, made explicit instead of eyeballed.
func assertDelta(label string, before, after, want counts) {
	got := counts{
		Ready:                after.Ready - before.Ready,
		Inflight:             after.Inflight - before.Inflight,
		Dead:                 after.Dead - before.Dead,
		OpenLeases:           after.OpenLeases - before.OpenLeases,
		AbandonedOutstanding: after.AbandonedOutstanding - before.AbandonedOutstanding,
		AbandonedTotal:       after.AbandonedTotal - before.AbandonedTotal,
	}
	if got != want {
		die(fmt.Sprintf("%s: delta = %+v, want %+v (either a blind spot -- a number that should have moved didn't -- or an unwanted side effect)", label, got, want))
	}
	fmt.Printf("  ✓ %s: delta = %+v (exactly as expected, nothing else moved)\n\n", label, got)
}

// ---- helpers ----

// runProcessUntil drives wc.Process in the background until condition
// reports done (polled every 20ms), then cancels and waits for it to return.
// Replaces the old CursorClaim's single-shot determinism now that claiming
// and processing overlap concurrently instead of running one range at a time.
func runProcessUntil(ctx context.Context, wc *consumer.MessageConsumer[common.Work], consumerFunc consumer.ConsumerFunc[common.Work], timeout time.Duration, done func() bool) time.Duration {
	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() { errCh <- wc.Consume(runCtx, consumerFunc) }()

	start := time.Now()
	for !done() {
		if time.Since(start) > timeout {
			cancel()
			die(fmt.Sprintf("timed out waiting for the expected condition, Process returned: %v", <-errCh))
		}
		time.Sleep(20 * time.Millisecond)
	}
	elapsed := time.Since(start)

	cancel()
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		die(fmt.Sprintf("Process returned an unexpected error: %v", err))
	}
	return elapsed
}

func seed(ctx context.Context, wp *producer.Producer[common.Work], n int) {
	for range n {
		_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
}

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }
func must(err error) {
	if err != nil {
		die(err.Error())
	}
}
func die(msg string) {
	fmt.Printf("\n❌ LAB FAILED: %s\n", msg)
	os.Exit(1)
}
func assert(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
