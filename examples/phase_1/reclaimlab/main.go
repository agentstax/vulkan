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
	iCommon "github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	cursoradvancerdatastore "github.com/agentstax/vulkan/pkg/consumergroup/cursoradvancer/controller/datastore"
	messageconsumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const (
	group    = "phase65b.lab"
	seedRows = 20
)

// set by main from RegisterGroup -- helpers are id-keyed
var groupId int64

func main() {
	if err := run(); err != nil {
		fmt.Printf("\n❌ LAB FAILED: %s\n", err.Error())
		os.Exit(1)
	}
}

// labFailure is what die panics with; run recovers it into its error so
// main's deferred cleanup runs on a failed assertion.
type labFailure struct {
	message string
}

func (f labFailure) Error() string {
	return f.message
}

func run() (err error) {
	defer func() {
		switch recovered := recover().(type) {
		case nil:
		case labFailure:
			err = recovered
		default:
			panic(recovered)
		}
	}()
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("phase65b.reclaimlab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true}))
	}()

	cd, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	messageConsumers, err := messageconsumergroupcontroller.NewMessageConsumerGroupController(ds, nil)
	must(err)
	cursorAdvancerDatastore, err := cursoradvancerdatastore.NewCursorAdvancerDatastore(ds, nil)
	must(err)
	wp, err := producer.NewProducer(ds, nil)
	must(err)
	wpInstance, err := wp.Register[common.Work](ctx, tp.Name)
	must(err)

	groupId = mustGroupID(cd.RegisterGroup(ctx, tp.Id, group, consumergroup.Beginning()))
	for range seedRows {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
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
	claim1, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, batch, maxRangeReclaims, lease, topic.DeliveryLogModeFailures)
	must(err)
	if claim1 == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v  lease=%s\n",
		claim1.Lease.Low, claim1.Lease.High, ids(claim1.Messages), shortTok(claim1.Lease.Token))
	committed := advance(ctx, cursorAdvancerDatastore, tp.Id) // the lazy roller ticks while the range is in-flight
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
	claim2, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, batch, maxRangeReclaims, lease, topic.DeliveryLogModeFailures)
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
	committed = advance(ctx, cursorAdvancerDatastore, tp.Id)
	fmt.Printf("  roller tick (mid-reclaim) -> committed = %d\n", committed)
	assert("committed still pinned during reclaim", committedCol(ctx, ds, tp.Id), claim1.Lease.Low)

	// the dead WORKER 1 "resurrects" and tries to commit with its STALE token: rejected
	if err := messageConsumers.Commit(ctx, tp.Id, groupId, claim1.Lease.Token, nil, 5*time.Second, topic.DeliveryLogModeFailures); !errors.Is(err, iCommon.ErrLeaseLost) {
		die(fmt.Sprintf("stale commit: want ErrLeaseLost, got %v", err))
	}
	assert("stale commit freed nothing (live lease survives)", leases(ctx, ds, tp.Id), 1)
	assert("stale commit did not move committed", committedCol(ctx, ds, tp.Id), claim1.Lease.Low)
	fmt.Println("  dead worker's stale Commit was rejected with ErrLeaseLost")

	// WORKER 2 finishes the range for real -> free lease, roller advances
	must(messageConsumers.Commit(ctx, tp.Id, groupId, claim2.Lease.Token, nil, 5*time.Second, topic.DeliveryLogModeFailures))
	committed = advance(ctx, cursorAdvancerDatastore, tp.Id)
	fmt.Printf("  reclaim committed -> roller tick -> committed = %d\n", committed)

	snapshot(ctx, ds, tp.Id, "AFTER RECLAIM COMMITTED")
	assert("committed released past reclaimed range", committedCol(ctx, ds, tp.Id), claim1.Lease.High)
	assert("crashed lease is gone", leases(ctx, ds, tp.Id), 0)
	assert("still no exception rows", deliveries(ctx, ds, tp.Id), 0)

	// ===== drain the rest so committed reaches head =====
	step("drain remaining ranges -> committed reaches head")
	for range 10 {
		c, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, batch, maxRangeReclaims, lease, topic.DeliveryLogModeFailures)
		must(err)
		if c == nil {
			break // caught up
		}
		must(messageConsumers.Commit(ctx, tp.Id, groupId, c.Lease.Token, nil, 5*time.Second, topic.DeliveryLogModeFailures))
		fmt.Printf("  drained (%d,%d] -> committed = %d\n", c.Lease.Low, c.Lease.High, advance(ctx, cursorAdvancerDatastore, tp.Id))
	}
	assert("committed reached head", committedCol(ctx, ds, tp.Id), head)
	assert("no leases left open", leases(ctx, ds, tp.Id), 0)
	assert("exception queue stayed empty the whole lab", deliveries(ctx, ds, tp.Id), 0)

	fmt.Println("\n✅ PHASE 6.5b LAB PASSED")
	fmt.Println("   crash mid-range -> lease expired -> exact range reclaimed (token rotated) ->")
	fmt.Println("   reprocessed -> committed pinned at lo then jumped to head -> exception queue empty.")
	return nil
}

// ---- helpers ----

func advance(ctx context.Context, cursorAdvancerDatastore *cursoradvancerdatastore.CursorAdvancerDatastore, topicId int64) int64 {
	c, err := cursorAdvancerDatastore.AdvanceCommitted(ctx, topicId, groupId)
	must(err)
	return c
}

func snapshot(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, label string) {
	fmt.Printf("  [%s] committed=%d claimed=%d open_leases=%d deliveries=%d\n",
		label, committedCol(ctx, ds, topicId), claimedCol(ctx, ds, topicId), leases(ctx, ds, topicId), deliveries(ctx, ds, topicId))
}

func committedCol(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT committed FROM consumer_group_cursor_%d WHERE consumer_group_id=$1`, topicId), groupId)
}
func claimedCol(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT claimed FROM consumer_group_cursor_%d WHERE consumer_group_id=$1`, topicId), groupId)
}
func leases(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM claim_lease_%d WHERE consumer_group_id=$1`, topicId), groupId)
}
func deliveries(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM exception_queue_%d WHERE consumer_group_id=$1`, topicId), groupId)
}

func scalar(ctx context.Context, ds *iDatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func ids(msgs []messageconsumergroupcontroller.Message) []int64 {
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
	panic(labFailure{message: msg})
}
func assert(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}

func mustGroupID(g *consumergroup.Group, err error) int64 { must(err); return g.Id }
