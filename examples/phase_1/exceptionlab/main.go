package main

// Phase 6.5c lab: watch the waterline pin on a failing message, then jump past it.
//
// Registers its own topic (destroyed on exit) and seeds it with 20 messages,
// so the lab is fully self-contained -- no dependency on a pre-seeded shared
// message_log the way the pre-8b version needed (`just produce 20` first).
//
// Drives the real datastore methods directly (Commit, AdvanceWaterline,
// ClaimExceptions, RecordExceptionSuccess) so the pin/jump is deterministic and
// asserted on exact cursor state, not inferred from timing.
//
// Confirms: a parked exception pins committed below it even while LATER ranges
// keep claiming and committing fine (the exception window never blocks fresh
// range claims), and once the exception resolves, committed jumps straight past
// it to catch up with claimed.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	group    = "phase65c.lab"
	seedRows = 20
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("phase65c.exceptionlab.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds)
	pd := producer.NewProducerDatastore[common.Work](ds)
	wp := producer.NewWorkProducer(tp, pd)

	must(cd.UpsertCursor(ctx, tp.Id, group))
	for range seedRows {
		_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
	head := scalar(ctx, ds, fmt.Sprintf(`SELECT COALESCE(max(id),0) FROM message_log_%d`, tp.Id))
	fmt.Printf("topic=%q id=%d message_log head = %d, group = %q\n", topicName, tp.Id, head, group)

	const lease = 5 * time.Second
	const batch = 5
	const maxRangeReclaims = 3 // never hit in this lab -- no crashed/reclaimed ranges here

	// ===== range 1: message 3 fails, the rest succeed =====
	step("claim range 1 (ids 1-5), message 3 fails processing")
	claim1, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, batch, maxRangeReclaims, lease)
	must(err)
	if claim1 == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim1.Lease.Low, claim1.Lease.High, ids(claim1.Messages))

	const failingId = int64(3)
	exceptions := []consumer.MessageException{{MessageId: failingId, Err: "simulated processing failure"}}
	must(cd.Commit(ctx, tp.Id, group, claim1.Lease.Token, exceptions, nil, 5*time.Second))
	assert("one parked exception", deliveries(ctx, ds, tp.Id), 1)

	committed := advance(ctx, cd, tp.Id)
	fmt.Printf("  roller tick -> committed = %d\n", committed)
	assert("committed pins below the failing message", committedCol(ctx, ds, tp.Id), failingId-1)

	// ===== range 2: fully succeeds, but committed stays pinned on message 3 =====
	step("claim + commit range 2 (ids 6-10), all succeed")
	claim2, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, batch, maxRangeReclaims, lease)
	must(err)
	if claim2 == nil {
		die("expected a fresh claim, got nil")
	}
	must(cd.Commit(ctx, tp.Id, group, claim2.Lease.Token, nil, nil, 5*time.Second))
	committed = advance(ctx, cd, tp.Id)
	fmt.Printf("  claimed (%d,%d], committed after roller tick = %d\n", claim2.Lease.Low, claim2.Lease.High, committed)
	assert("claimed moved past the pin", claimedCol(ctx, ds, tp.Id), claim2.Lease.High)
	assert("committed still pinned on the unresolved exception", committedCol(ctx, ds, tp.Id), failingId-1)
	fmt.Println("  -> a parked exception never blocks fresh ranges from claiming/committing, only the waterline")

	// Commit's park always sets an initial 5s can_run_after -- the exception isn't
	// claimable until that backoff passes, same as reclaimlab's lease-expiry wait.
	step("sleep 5.5s — let the parked exception's initial backoff pass")
	time.Sleep(5500 * time.Millisecond)

	// ===== drain the exception window: message 3 retried and succeeds =====
	step("ClaimExceptions drains message 3, retry succeeds")
	claimedExceptions, err := cd.ClaimExceptions(ctx, tp.Id, group, batch, 3, lease)
	must(err)
	if len(claimedExceptions) != 1 || claimedExceptions[0].MessageId != failingId {
		die(fmt.Sprintf("expected to claim exactly message %d, got %+v", failingId, claimedExceptions))
	}
	fmt.Printf("  claimed exception message_id=%d attempts=%d\n", claimedExceptions[0].MessageId, claimedExceptions[0].Attempts)
	must(cd.RecordExceptionSuccess(ctx, &claimedExceptions[0]))
	assert("exception pop-deleted on success", deliveries(ctx, ds, tp.Id), 0)

	// ===== committed jumps straight past the resolved exception =====
	step("roller tick — committed jumps past the resolved exception")
	committed = advance(ctx, cd, tp.Id)
	fmt.Printf("  committed = %d\n", committed)
	assert("committed jumped to claimed", committedCol(ctx, ds, tp.Id), claimedCol(ctx, ds, tp.Id))

	// ===== drain the rest so committed reaches head =====
	step("drain remaining ranges -> committed reaches head")
	for range 10 {
		c, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, batch, maxRangeReclaims, lease)
		must(err)
		if c == nil {
			break // caught up
		}
		must(cd.Commit(ctx, tp.Id, group, c.Lease.Token, nil, nil, 5*time.Second))
		fmt.Printf("  drained (%d,%d] -> committed = %d\n", c.Lease.Low, c.Lease.High, advance(ctx, cd, tp.Id))
	}
	assert("committed reached head", committedCol(ctx, ds, tp.Id), head)
	assert("no deliveries left behind", deliveries(ctx, ds, tp.Id), 0)

	fmt.Println("\n✅ PHASE 6.5c LAB PASSED")
	fmt.Println("   failure parked as an exception -> waterline pinned below it while later ranges")
	fmt.Println("   kept committing -> exception resolved -> waterline jumped straight past it.")
}

// ---- helpers ----

func advance(ctx context.Context, cd consumer.Datastore[common.Work], topicID int64) int64 {
	c, err := cd.AdvanceWaterline(ctx, topicID, group)
	must(err)
	return c
}

func committedCol(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, `SELECT committed FROM cursors WHERE consumer_group=$1 AND topic_id=$2`, group, topicID)
}
func claimedCol(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, `SELECT claimed FROM cursors WHERE consumer_group=$1 AND topic_id=$2`, group, topicID)
}
func deliveries(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, `SELECT count(*) FROM deliveries WHERE consumer_group=$1 AND topic_id=$2`, group, topicID)
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func ids(msgs []consumer.MessageRow) []int64 {
	out := make([]int64, len(msgs))
	for i, m := range msgs {
		out[i] = m.Id
	}
	return out
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
