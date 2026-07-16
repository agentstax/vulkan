package main

// Phase 9 lab: graceful-shutdown lease truncation.
//
// CursorClaim's per-message loop now checks ctx.Done() between messages (not
// mid-message -- a hard per-message timeout is a separate, not-yet-built item). An
// interruption mid-range must not force the WHOLE range to sit out a full
// lease-expiry reclaim: everything already resolved (successes + a parked
// exception) has to survive, and only the untouched suffix should remain leased
// for a future reclaim.
//
// Drives CursorClaim directly (not Consume) so cancellation timing is
// deterministic: a consumerFunc closure cancels the shared context partway
// through a batch, simulating a shutdown signal arriving mid-range.
//
// Confirms:
//   - messages before the interruption point resolve normally (one success, one
//     parked exception) and are never re-attempted
//   - the message after the interruption point is never even attempted
//   - the lease survives, narrowed to (lastProcessed, high] -- not deleted, not
//     left spanning the whole original range
//   - AdvanceWaterline stays correctly pinned behind the unresolved exception even
//     though the lease is already narrowed past it (the two blockers combine via
//     LEAST, neither overrides the other)
//   - once the exception resolves, the waterline advances to the narrowed low --
//     it does NOT need the untouched suffix's lease to expire first
//   - once that narrowed lease naturally expires, ONLY the untouched suffix is
//     reclaimed -- the resolved prefix is never redelivered

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
	"github.com/jackc/pgx/v5"
)

const group = "phase9.shutdowntruncationlab"

// WorkTimeout + QueueTimeout + AckMargin below sums to this -- kept equal to the
// leaseDuration used for the manual reclaim call later, so both claims behave
// the same way.
const lease = 2 * time.Second

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("phase9.shutdowntruncationlab.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)
	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)

	must(cd.UpsertCursor(ctx, tp.Id, group))
	seed(ctx, wp, 3)

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](10)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(1)
	must(err)

	wc, err := consumer.NewWorkConsumer[common.Work](group, tp, queue, pool, ds, &consumer.WorkConsumerConfig{
		BatchLimit:   3,
		WorkTimeout:  1 * time.Second,
		QueueTimeout: 500 * time.Millisecond,
		AckMargin:    500 * time.Millisecond, // also PartialCommit's own detached-ctx budget
	})
	must(err)

	step("WORKER claims all 3, shutdown fires after message 2 -- message 3 never attempted")
	runCtx, cancel := context.WithCancel(ctx)
	calls := 0
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		calls++
		switch calls {
		case 1:
			return nil // success
		case 2:
			cancel() // shutdown signal arrives -- this message still finishes though
			return errors.New("simulated failure")
		default:
			die(fmt.Sprintf("consumerFunc called a %dth time -- message 3 must never be attempted", calls))
			return nil
		}
	}
	must(wc.CursorClaim(runCtx, consumerFunc))
	assert("exactly 2 messages attempted", int64(calls), 2)

	lb := onlyLease(ctx, ds, tp.Id)
	fmt.Printf("  lease narrowed: (%d,%d] (was (0,%d])\n", lb.low, lb.high, lb.high)
	assert("lease survives (not deleted)", leases(ctx, ds, tp.Id), 1)
	assert("lease high unchanged", lb.high, 3)
	assert("lease low narrowed to message 2", lb.low, 2)
	assert("exactly 1 parked exception (message 2)", deliveries(ctx, ds, tp.Id), 1)
	assertStatus(ctx, ds, tp.Id, 2, "ready")

	step("waterline stays pinned behind the unresolved exception, even though the lease is already narrowed past it")
	committed := advance(ctx, cd, tp.Id)
	assert("committed blocked at message 1 (exception at 2 still unresolved)", committed, 1)

	step("sleep 5.5s — let the parked exception's initial backoff pass")
	time.Sleep(5500 * time.Millisecond)

	step("resolve the exception -- waterline jumps to the narrowed low, no need to wait on the untouched suffix's lease")
	claimedExceptions, err := cd.ClaimExceptions(ctx, tp.Id, group, 10, 3, lease, false)
	must(err)
	if len(claimedExceptions) != 1 {
		die(fmt.Sprintf("expected 1 claimed exception, got %d", len(claimedExceptions)))
	}
	must(cd.RecordExceptionSuccess(ctx, &claimedExceptions[0]))
	committed = advance(ctx, cd, tp.Id)
	assert("committed advances to the narrowed low", committed, 2)
	assert("deliveries drained (exception pop-deleted)", deliveries(ctx, ds, tp.Id), 0)

	// the narrowed lease's 2s duration already elapsed during the 5.5s backoff
	// sleep above -- no separate wait needed before reclaiming it.
	step("reclaim: only the untouched suffix comes back, not the resolved prefix")
	claim2, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, 3, 3, lease, false)
	must(err)
	if claim2 == nil {
		die("expected a reclaim, got nil")
	}
	assert("reclaimed range starts at the narrowed low", claim2.Lease.Low, 2)
	assert("reclaimed range ends at the original high", claim2.Lease.High, 3)
	assert("reclaimed exactly the untouched suffix (1 message)", int64(len(claim2.Messages)), 1)
	assert("reclaimed message is the one never attempted", claim2.Messages[0].Id, 3)

	must(cd.Commit(ctx, tp.Id, group, claim2.Lease.Token, nil, nil, 5*time.Second, false))
	committed = advance(ctx, cd, tp.Id)
	assert("committed reaches head", committed, 3)
	assert("no leases left open", leases(ctx, ds, tp.Id), 0)

	fmt.Println("\n✅ SHUTDOWN LEASE TRUNCATION LAB PASSED")
	fmt.Println("   an interruption mid-range parks what resolved and narrows the lease to the")
	fmt.Println("   untouched suffix -- the resolved prefix is never redelivered, the waterline's")
	fmt.Println("   exception-blocker and lease-narrowing terms combine correctly via LEAST, and")
	fmt.Println("   the untouched suffix reclaims on its own once its (now-shorter) lease expires.")
}

// ---- helpers ----

func seed(ctx context.Context, wp *producer.WorkProducer[common.Work], n int) {
	for range n {
		_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
}

func advance(ctx context.Context, cd consumer.Datastore[common.Work], topicID int64) int64 {
	c, err := cd.AdvanceWaterline(ctx, topicID, group)
	must(err)
	return c
}

type leaseBounds struct{ low, high int64 }

func onlyLease(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) leaseBounds {
	var lb leaseBounds
	must(ds.Pool.QueryRow(ctx, `SELECT low, high FROM lease WHERE consumer_group=$1 AND topic_id=$2`, group, topicID).Scan(&lb.low, &lb.high))
	return lb
}

func leases(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, `SELECT count(*) FROM lease WHERE consumer_group=$1 AND topic_id=$2`, group, topicID)
}
func deliveries(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM delivery_%d WHERE consumer_group=$1`, topicID), group)
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func assertStatus(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, messageID int64, want string) {
	var got string
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT status FROM delivery_%d WHERE consumer_group=$1 AND message_id=$2`, topicID), group, messageID).Scan(&got))
	if got != want {
		die(fmt.Sprintf("message %d status: got %q, want %q", messageID, got, want))
	}
	fmt.Printf("  ✓ message %d status = %q\n", messageID, got)
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
