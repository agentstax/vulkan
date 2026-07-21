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
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const (
	group    = "phase10.metricsreactionlab"
	seedRows = 9
	batch    = 3
	lease    = 3 * time.Second
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("%s.%d", group, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, &topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	wp, err := producer.NewMessageProducer[common.Work](tp, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))
	seed(ctx, wp, seedRows)

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](30)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(3)
	must(err)

	wc, err := consumer.NewMessageConsumer[common.Work](group, tp, queue, pool, ds, &consumer.MessageConsumerConfig{
		BatchLimit:              batch,
		WorkTimeout:             1 * time.Second,
		WorkTimeoutGrace:        100 * time.Millisecond,
		ExceptionInitialBackoff: 300 * time.Millisecond,
	})
	must(err)
	must(wc.Register(ctx))

	scenarioFailRate(ctx, wc)
	scenarioExhaustedRetries(ctx, wc)
	scenarioHardTimeout(ctx, wc)
	scenarioCrash(ctx, wc)

	fmt.Println("\n✅ METRICS REACTION LAB PASSED")
	fmt.Println("   every induced failure mode moved exactly the snapshot number(s) it should have -- no blind spots.")
}

// ---- scenario 1: retryable failure (--fail-rate) ----

const failingId1 = int64(1)

func scenarioFailRate(ctx context.Context, wc *consumer.MessageConsumer[common.Work]) {
	step("SCENARIO 1: retryable failure (--fail-rate equivalent) -> ready exception")
	before := snapshotCounts(ctx, wc)

	calls := 0
	must(wc.CursorClaim(ctx, func(ctx context.Context, work *common.Work) error {
		calls++
		if calls == 1 {
			return errors.New("artificial failure from -fail-rate")
		}
		return nil
	}))
	assert("all 3 messages in range 1 attempted", int64(calls), 3)

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
	calls := 0
	start := time.Now()
	must(wc.CursorClaim(ctx, func(ctx context.Context, work *common.Work) error {
		calls++
		if calls == 1 {
			time.Sleep(hangFor)
		}
		return nil
	}))
	elapsed := time.Since(start)
	if elapsed >= hangFor {
		die(fmt.Sprintf("CursorClaim took %s -- it should have abandoned message 4 around WorkTimeout+Grace, not waited out the full %s hang", elapsed, hangFor))
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

func seed(ctx context.Context, wp *producer.MessageProducer[common.Work], n int) {
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
