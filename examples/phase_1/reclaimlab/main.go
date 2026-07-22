package main

// Phase 6.5b lab: crash mid-range, recover.
//
// Registers its own topic (destroyed on exit) and seeds it with 20 messages,
// so the lab is fully self-contained -- no dependency on a pre-seeded shared
// message_log the way the pre-8b version needed (`just produce 20` first).
//
// Drives the real datastore methods directly so a "crash" is deterministic: a
// worker claims a range (which opens a lease) and then simply never Commits it --
// exactly what a process that dies mid-range leaves behind. A short lease lets the
// lab show the expiry + reclaim without real-time waiting.
//
// Confirms: no exception rows are written, committed stays pinned at the crashed
// range's lo, Reclaim re-reads the EXACT range with a ROTATED token (so the dead
// worker's later commit no-ops), and committed jumps to head once the reclaim
// completes.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const (
	group    = "phase65b.lab"
	seedRows = 20
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

	topicName := fmt.Sprintf("phase65b.reclaimlab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topic.Config{})
	must(err)
	defer func() { must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true})) }()

	cd, err := consumer.NewConsumerDatastore[common.Work](ds, nil)
	must(err)
	wp, err := producer.NewMessageProducer[common.Work](tp, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	must(cd.UpsertCursor(ctx, tp.Id, group))
	for range seedRows {
		_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
	head := scalar(ctx, ds, fmt.Sprintf(`SELECT COALESCE(max(id),0) FROM message_log_%d`, tp.Id))
	fmt.Printf("topic=%q id=%d message_log head = %d, group = %q\n", topicName, tp.Id, head, group)

	const lease = 2 * time.Second
	const batch = 10
	const maxRangeReclaims = 3 // this lab reclaims exactly once -- never enough to quarantine

	// ===== WORKER 1: claim a range, tick the roller, then CRASH (never commit) =====
	step("WORKER 1 claims a range, then crashes mid-range (never Commit)")
	claim1, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, batch, maxRangeReclaims, lease, false)
	must(err)
	if claim1 == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v  lease=%s\n",
		claim1.Lease.Low, claim1.Lease.High, ids(claim1.Messages), shortTok(claim1.Lease.Token))
	committed := advance(ctx, cd, tp.Id) // the lazy roller ticks while the range is in-flight
	fmt.Printf("  roller tick -> committed = %d\n", committed)
	// *** CRASH: control never reaches Commit(claim1) ***
	oldTok := shortTok(claim1.Lease.Token)

	snapshot(ctx, ds, tp.Id, "AFTER CRASH")
	assert("no exception rows written", deliveries(ctx, ds, tp.Id), 0)
	assert("committed pinned at range lo", committedCol(ctx, ds, tp.Id), claim1.Lease.Low)
	assert("claimed sits at range hi", claimedCol(ctx, ds, tp.Id), claim1.Lease.High)
	assert("exactly one open lease", leases(ctx, ds, tp.Id), 1)

	// ===== lease expiry =====
	step(fmt.Sprintf("sleep %s — let the crashed lease expire", lease+500*time.Millisecond))
	time.Sleep(lease + 500*time.Millisecond)

	// ===== WORKER 2: Reclaim-before-Claim grabs the EXACT expired range =====
	step("WORKER 2 polls: Reclaim-before-Claim picks up the expired lease")
	claim2, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, batch, maxRangeReclaims, lease, false)
	must(err)
	if claim2 == nil {
		die("expected a reclaim, got nil")
	}
	fmt.Printf("  reclaimed (%d,%d]  ids=%v  NEW lease=%s (was %s)\n",
		claim2.Lease.Low, claim2.Lease.High, ids(claim2.Messages), shortTok(claim2.Lease.Token), oldTok)
	assert("reclaim re-reads exact range lo", claim2.Lease.Low, claim1.Lease.Low)
	assert("reclaim re-reads exact range hi", claim2.Lease.High, claim1.Lease.High)
	assert("reclaim re-reads same message count", int64(len(claim2.Messages)), int64(len(claim1.Messages)))
	if shortTok(claim2.Lease.Token) == oldTok {
		die("token was NOT rotated — R5 violated")
	}
	fmt.Println("  token rotated -> the dead worker's stale commit will now no-op")

	// committed is still pinned at lo while the reclaimed range is in-flight again
	committed = advance(ctx, cd, tp.Id)
	fmt.Printf("  roller tick (mid-reclaim) -> committed = %d\n", committed)
	assert("committed still pinned during reclaim", committedCol(ctx, ds, tp.Id), claim1.Lease.Low)

	// the dead WORKER 1 "resurrects" and tries to commit with its STALE token: rejected
	if err := cd.Commit(ctx, tp.Id, group, claim1.Lease.Token, nil, nil, 5*time.Second, false); !errors.Is(err, consumer.ErrLeaseLost) {
		die(fmt.Sprintf("stale commit: want ErrLeaseLost, got %v", err))
	}
	assert("stale commit freed nothing (live lease survives)", leases(ctx, ds, tp.Id), 1)
	assert("stale commit did not move the waterline", committedCol(ctx, ds, tp.Id), claim1.Lease.Low)
	fmt.Println("  dead worker's stale Commit was rejected with ErrLeaseLost")

	// WORKER 2 finishes the range for real -> free lease, roller advances
	must(cd.Commit(ctx, tp.Id, group, claim2.Lease.Token, nil, nil, 5*time.Second, false))
	committed = advance(ctx, cd, tp.Id)
	fmt.Printf("  reclaim committed -> roller tick -> committed = %d\n", committed)

	snapshot(ctx, ds, tp.Id, "AFTER RECLAIM COMMITTED")
	assert("committed released past reclaimed range", committedCol(ctx, ds, tp.Id), claim1.Lease.High)
	assert("crashed lease is gone", leases(ctx, ds, tp.Id), 0)
	assert("still no exception rows", deliveries(ctx, ds, tp.Id), 0)

	// ===== drain the rest so committed reaches head =====
	step("drain remaining ranges -> committed reaches head")
	for range 10 {
		c, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, batch, maxRangeReclaims, lease, false)
		must(err)
		if c == nil {
			break // caught up
		}
		must(cd.Commit(ctx, tp.Id, group, c.Lease.Token, nil, nil, 5*time.Second, false))
		fmt.Printf("  drained (%d,%d] -> committed = %d\n", c.Lease.Low, c.Lease.High, advance(ctx, cd, tp.Id))
	}
	assert("committed reached head", committedCol(ctx, ds, tp.Id), head)
	assert("no leases left open", leases(ctx, ds, tp.Id), 0)
	assert("delivery table stayed empty the whole lab", deliveries(ctx, ds, tp.Id), 0)

	fmt.Println("\n✅ PHASE 6.5b LAB PASSED")
	fmt.Println("   crash mid-range -> lease expired -> exact range reclaimed (token rotated) ->")
	fmt.Println("   reprocessed -> waterline pinned at lo then jumped to head -> delivery table empty.")
}

// ---- helpers ----

func advance(ctx context.Context, cd *consumer.ConsumerDatastore[common.Work], topicID int64) int64 {
	c, err := cd.AdvanceWaterline(ctx, topicID, group)
	must(err)
	return c
}

func snapshot(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, label string) {
	fmt.Printf("  [%s] committed=%d claimed=%d open_leases=%d deliveries=%d\n",
		label, committedCol(ctx, ds, topicID), claimedCol(ctx, ds, topicID), leases(ctx, ds, topicID), deliveries(ctx, ds, topicID))
}

func committedCol(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, `SELECT committed FROM cursor WHERE consumer_group=$1 AND topic_id=$2`, group, topicID)
}
func claimedCol(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, `SELECT claimed FROM cursor WHERE consumer_group=$1 AND topic_id=$2`, group, topicID)
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

func ids(msgs []consumer.MessageRow) []int64 {
	out := make([]int64, len(msgs))
	for i, m := range msgs {
		out[i] = m.Id
	}
	return out
}

func shortTok[T fmt.Stringer](t T) string {
	s := t.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
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
