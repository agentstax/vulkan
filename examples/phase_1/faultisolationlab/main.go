package main

// Phase 9 lab: induce each of the three failure modes callSafely was built to
// contain, through the real CursorClaim path against real Postgres, plus a
// direct check of the pkg/retry mechanism a DB blip depends on.
//
// 1. PANIC -- one message's consumerFunc panics. Confirms recover() converts
//    it into an ordinary parked exception (not a process crash), the other
//    two messages in the same batch are unaffected, and the panic message +
//    stack trace land in last_error.
//
// 2. HARD TIMEOUT -- one message hangs well past WorkTimeout+WorkTimeoutGrace.
//    Confirms CursorClaim doesn't wait out the whole hang before moving on to
//    the next message, the hung message is parked as a retryable exception,
//    the abandoned-goroutine gauge shows it while it's still running in the
//    background, and the gauge/reclaim-latency correctly update once that
//    detached goroutine finally finishes on its own.
//
// 3. DB BLIP -- a direct check of pkg/retry.DatastoreRetry.Wrap (the
//    mechanism CursorClaim's own datastore calls depend on): a transient
//    failure that clears within MaxRetries must be fully invisible to the
//    caller (Wrap returns nil), and one that never clears must still fail
//    once retries are exhausted, not retry forever. Building this lab is
//    what surfaced a real bug in Wrap -- see NOTES.md/LEARNING_PLAN.md.

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	runPanicIsolation(ctx, ds)
	runHardTimeoutAbandon(ctx, ds)
	runDBBlipRecovery(ctx)

	fmt.Println("\n✅ FAULT ISOLATION LAB PASSED")
	fmt.Println("   a panic, a hard timeout, and a DB blip are each contained to the one message")
	fmt.Println("   or one call they happened on -- none of them take down the range, the consumer,")
	fmt.Println("   or the caller of a retry that ultimately succeeds.")
}

// ---- scenario 1: panic ----

func runPanicIsolation(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("PANIC -- one message's consumerFunc panic is isolated to that message")

	group := "phase9.faultisolationlab.panic"
	topicName := fmt.Sprintf("%s.%d", group, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds)
	pd := producer.NewProducerDatastore[common.Work](ds)
	wp := producer.NewWorkProducer(tp, pd)

	must(cd.UpsertCursor(ctx, tp.Id, group))
	seed(ctx, wp, 3)

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](10)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(1)
	must(err)

	wc := consumer.NewWorkConsumer[common.Work](group, tp, queue, pool, cd, &consumer.WorkConsumerConfig{
		BatchLimit:       3,
		WorkTimeout:      5 * time.Second,
		WorkTimeoutGrace: 100 * time.Millisecond,
	})

	calls := 0
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		calls++
		switch calls {
		case 1:
			return nil
		case 2:
			panic("simulated consumerFunc panic")
		case 3:
			return nil
		default:
			die(fmt.Sprintf("consumerFunc called a %dth time -- only 3 messages seeded", calls))
			return nil
		}
	}

	must(wc.CursorClaim(ctx, consumerFunc))
	assert("all 3 messages attempted -- one panic doesn't stop the batch", int64(calls), 3)
	assert("exactly 1 parked exception (the panicking message)", deliveries(ctx, ds, tp.Id, group), 1)
	assertStatus(ctx, ds, tp.Id, group, 2, "ready")
	assertLastErrorContains(ctx, ds, tp.Id, group, 2, "recovered from consumerFunc panic")
	assert("range fully committed -- no leases left open", leases(ctx, ds, tp.Id, group), 0)

	committed := advance(ctx, cd, tp.Id, group)
	assert("waterline pinned behind the unresolved exception, despite message 3 succeeding", committed, 1)

	fmt.Println("  ✓ panic isolated to message 2 -- messages 1 and 3 processed normally, range still committed")
}

// ---- scenario 2: hard timeout / abandoned goroutine ----

func runHardTimeoutAbandon(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("HARD TIMEOUT -- one message hangs past WorkTimeout, gets abandoned+retried without blocking the others")

	group := "phase9.faultisolationlab.hang"
	topicName := fmt.Sprintf("%s.%d", group, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds)
	pd := producer.NewProducerDatastore[common.Work](ds)
	wp := producer.NewWorkProducer(tp, pd)

	must(cd.UpsertCursor(ctx, tp.Id, group))
	seed(ctx, wp, 3)

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](10)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(1)
	must(err)

	wc := consumer.NewWorkConsumer[common.Work](group, tp, queue, pool, cd, &consumer.WorkConsumerConfig{
		BatchLimit:       3,
		WorkTimeout:      1 * time.Second,
		WorkTimeoutGrace: 100 * time.Millisecond,
	})
	// leaseDuration = WorkTimeout+QueueTimeout+AckMargin (defaults: 1s+5s+2s=8s)
	// stays well above hangFor below, so the lease itself never expires mid-test.

	const hangFor = 2500 * time.Millisecond
	calls := 0
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		calls++
		switch calls {
		case 1:
			return nil
		case 2:
			time.Sleep(hangFor) // deliberately outlives WorkTimeout+WorkTimeoutGrace -- gets abandoned
			return nil
		case 3:
			return nil
		default:
			die(fmt.Sprintf("consumerFunc called a %dth time -- only 3 messages seeded", calls))
			return nil
		}
	}

	start := time.Now()
	must(wc.CursorClaim(ctx, consumerFunc))
	elapsed := time.Since(start)
	fmt.Printf("  CursorClaim returned after %s (message 2's own goroutine is still sleeping in the background)\n", elapsed)

	if elapsed >= hangFor {
		die(fmt.Sprintf("CursorClaim took %s -- it should have abandoned message 2 around WorkTimeout+Grace (~1.1s), not waited out the full %s hang", elapsed, hangFor))
	}
	fmt.Printf("  ✓ CursorClaim returned well before the %s hang finished\n", hangFor)

	assert("all 3 messages attempted -- the hang doesn't block message 3", int64(calls), 3)
	assert("exactly 1 parked exception (the hung message)", deliveries(ctx, ds, tp.Id, group), 1)
	assertStatus(ctx, ds, tp.Id, group, 2, "ready")
	assertLastErrorContains(ctx, ds, tp.Id, group, 2, "hard timeout")
	assertLastErrorContains(ctx, ds, tp.Id, group, 2, "goroutine abandoned")

	assert("abandoned-goroutine gauge shows exactly 1 outstanding right after CursorClaim returns", int64(wc.Metrics.AbandonedRoutines.CurrentTotal()), 1)
	assert("tracking total reflects exactly 1 abandonment ever", int64(wc.Metrics.AbandonedRoutines.TrackingTotal()), 1)

	step("waiting for the abandoned goroutine to actually finish its sleep and self-reclaim")
	deadline := time.Now().Add(5 * time.Second)
	for wc.Metrics.AbandonedRoutines.CurrentTotal() > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	assert("gauge drops back to 0 once the abandoned goroutine finishes on its own", int64(wc.Metrics.AbandonedRoutines.CurrentTotal()), 0)

	if lat := wc.Metrics.AbandonedRoutines.ReclaimLatency(); lat <= 0 {
		die(fmt.Sprintf("expected a positive reclaim latency once the abandoned goroutine finished, got %s", lat))
	} else {
		fmt.Printf("  ✓ reclaim latency recorded: %s\n", lat)
	}

	fmt.Println("  ✓ hard timeout isolated to message 2 -- messages 1 and 3 processed without waiting on it, leaked goroutine tracked then self-reclaimed")
}

// ---- scenario 3: DB blip (pkg/retry directly -- no topic needed) ----

func runDBBlipRecovery(ctx context.Context) {
	step("DB BLIP -- pkg/retry absorbs transient failures transparently; the caller never sees an error once it clears")

	r := retry.NewDatastoreRetry(6, 10*time.Millisecond, 200*time.Millisecond, 2)

	calls := 0
	err := r.Wrap(ctx, func() error {
		calls++
		if calls <= 2 {
			return fakeNetError{} // looks like a transient network blip -- retryable
		}
		return nil // blip cleared
	})
	must(err)
	assert("retried through 2 injected blips before succeeding", int64(calls), 3)
	fmt.Println("  ✓ Wrap returned nil despite 2 injected transient failures -- caller never sees an error, consumer keeps running")

	step("DB BLIP (exhausted) -- if the blip never clears, Wrap still correctly fails after MaxRetries, not forever")
	calls2 := 0
	err2 := r.Wrap(ctx, func() error {
		calls2++
		return fakeNetError{}
	})
	if err2 == nil {
		die("expected a non-nil error once retries are exhausted, got nil")
	}
	assert("exhausted all 6 attempts", int64(calls2), 6)
	fmt.Println("  ✓ Wrap correctly fails once retries are exhausted -- a permanent blip doesn't retry forever")
}

// fakeNetError satisfies net.Error (Timeout/Temporary) without needing a real
// network fault -- retry.IsTransientPgError treats any net.Error as retryable.
type fakeNetError struct{}

func (fakeNetError) Error() string   { return "simulated transient network blip" }
func (fakeNetError) Timeout() bool   { return true }
func (fakeNetError) Temporary() bool { return true }

var _ net.Error = fakeNetError{}

// ---- helpers ----

func seed(ctx context.Context, wp *producer.WorkProducer[common.Work], n int) {
	for range n {
		_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
}

func advance(ctx context.Context, cd consumer.Datastore[common.Work], topicID int64, group string) int64 {
	c, err := cd.AdvanceWaterline(ctx, topicID, group)
	must(err)
	return c
}

func leases(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, group string) int64 {
	return scalar(ctx, ds, `SELECT count(*) FROM leases WHERE consumer_group=$1 AND topic_id=$2`, group, topicID)
}

func deliveries(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, group string) int64 {
	return scalar(ctx, ds, `SELECT count(*) FROM deliveries WHERE consumer_group=$1 AND topic_id=$2`, group, topicID)
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func assertStatus(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, group string, messageID int64, want string) {
	var got string
	must(ds.Pool.QueryRow(ctx, `SELECT status FROM deliveries WHERE consumer_group=$1 AND topic_id=$2 AND message_id=$3`, group, topicID, messageID).Scan(&got))
	if got != want {
		die(fmt.Sprintf("message %d status: got %q, want %q", messageID, got, want))
	}
	fmt.Printf("  ✓ message %d status = %q\n", messageID, got)
}

func assertLastErrorContains(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, group string, messageID int64, substr string) {
	var got string
	must(ds.Pool.QueryRow(ctx, `SELECT last_error FROM deliveries WHERE consumer_group=$1 AND topic_id=$2 AND message_id=$3`, group, topicID, messageID).Scan(&got))
	if !strings.Contains(got, substr) {
		die(fmt.Sprintf("message %d last_error %q does not contain %q", messageID, got, substr))
	}
	fmt.Printf("  ✓ message %d last_error contains %q\n", messageID, substr)
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
