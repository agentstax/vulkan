package main

// Phase 6.5c lab: watch committed pin on a failing message, then jump past it.
//
// Registers its own topic (destroyed on exit) and seeds it with 20 messages,
// so the lab is fully self-contained -- no dependency on a pre-seeded shared
// message_log the way the pre-8b version needed (`just produce 20` first).
//
// Drives the real datastore methods directly (Commit, AdvanceCommitted,
// ClaimExceptions, RecordExceptionSuccess) so the pin/jump is deterministic and
// asserted on exact cursor state, not inferred from timing.
//
// Confirms: an unresolved exception pins committed below it even while LATER ranges
// keep claiming and committing fine (the exception window never blocks fresh
// range claims), and once the exception resolves, committed jumps straight past
// it to catch up with claimed.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	cursoradvancerdatastore "github.com/agentstax/vulkan/pkg/consumergroup/cursoradvancer/controller/datastore"
	exceptionconsumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/exceptionconsumer/controller"
	messageconsumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
)

const (
	group    = "phase65c.lab"
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

	topicName := fmt.Sprintf("phase65c.exceptionlab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true}))
	}()

	cd, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	messageConsumers, err := messageconsumergroupcontroller.NewMessageConsumerGroupController(ds, nil)
	must(err)
	exceptionConsumers, err := exceptionconsumergroupcontroller.NewExceptionConsumerGroupController(ds, nil)
	must(err)
	cursorAdvancerDatastore, err := cursoradvancerdatastore.NewCursorAdvancerDatastore(ds, nil)
	must(err)
	wp, err := producer.NewProducer(ds, nil)
	must(err)
	wpInstance, err := wp.Register[common.Work](ctx, tp.Name)
	must(err)

	groupId = mustGroupID(cd.RegisterGroup(ctx, tp.Id, group, consumergroup.Beginning()))
	for range seedRows {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ string) (*common.Work, error) {
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
	claim1, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, batch, maxRangeReclaims, lease, topic.DeliveryLogModeFailures)
	must(err)
	if claim1 == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim1.Lease.Low, claim1.Lease.High, ids(claim1.Messages))

	const failingId = int64(3)
	exceptions := []messageconsumergroupcontroller.MessageOutcome{{MessageId: failingId, Kind: messageconsumergroupcontroller.OutcomeException, Err: "simulated processing failure"}}
	must(messageConsumers.Commit(ctx, tp.Id, groupId, claim1.Lease.Token, exceptions, 5*time.Second, topic.DeliveryLogModeFailures))
	assert("one unresolved exception", deliveries(ctx, ds, tp.Id), 1)

	committed := advance(ctx, cursorAdvancerDatastore, tp.Id)
	fmt.Printf("  roller tick -> committed = %d\n", committed)
	assert("committed pins below the failing message", committedCol(ctx, ds, tp.Id), failingId-1)

	// ===== range 2: fully succeeds, but committed stays pinned on message 3 =====
	step("claim + commit range 2 (ids 6-10), all succeed")
	claim2, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, batch, maxRangeReclaims, lease, topic.DeliveryLogModeFailures)
	must(err)
	if claim2 == nil {
		die("expected a fresh claim, got nil")
	}
	must(messageConsumers.Commit(ctx, tp.Id, groupId, claim2.Lease.Token, nil, 5*time.Second, topic.DeliveryLogModeFailures))
	committed = advance(ctx, cursorAdvancerDatastore, tp.Id)
	fmt.Printf("  claimed (%d,%d], committed after roller tick = %d\n", claim2.Lease.Low, claim2.Lease.High, committed)
	assert("claimed moved past the pin", claimedCol(ctx, ds, tp.Id), claim2.Lease.High)
	assert("committed still pinned on the unresolved exception", committedCol(ctx, ds, tp.Id), failingId-1)
	fmt.Println("  -> an unresolved exception never blocks fresh ranges from claiming/committing, only committed")

	// Commit's exception write always sets an initial 5s can_run_after -- the exception isn't
	// claimable until that backoff passes, same as reclaimlab's lease-expiry wait.
	step("sleep 5.5s — let the unresolved exception's initial backoff pass")
	time.Sleep(5500 * time.Millisecond)

	// ===== drain the exception window: message 3 retried and succeeds =====
	step("ClaimExceptions drains message 3, retry succeeds")
	claimedExceptions, err := exceptionConsumers.Claim(ctx, tp.Id, groupId, 1, batch, 3, lease, tp.DeliveryLogMode)
	must(err)
	if len(claimedExceptions) != 1 || claimedExceptions[0].MessageId != failingId {
		die(fmt.Sprintf("expected to claim exactly message %d, got %+v", failingId, claimedExceptions))
	}
	fmt.Printf("  claimed exception message_id=%d attempts=%d\n", claimedExceptions[0].MessageId, claimedExceptions[0].Attempts)
	must(exceptionConsumers.RecordSuccess(ctx, &claimedExceptions[0], tp.DeliveryLogMode, nil))
	assert("exception pop-deleted on success", deliveries(ctx, ds, tp.Id), 0)

	// ===== committed jumps straight past the resolved exception =====
	step("roller tick — committed jumps past the resolved exception")
	committed = advance(ctx, cursorAdvancerDatastore, tp.Id)
	fmt.Printf("  committed = %d\n", committed)
	assert("committed jumped to claimed", committedCol(ctx, ds, tp.Id), claimedCol(ctx, ds, tp.Id))

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
	assert("no deliveries left behind", deliveries(ctx, ds, tp.Id), 0)

	fmt.Println("\n✅ PHASE 6.5c LAB PASSED")
	fmt.Println("   failure recorded as an unresolved exception -> committed pinned below it while later ranges")
	fmt.Println("   kept committing -> exception resolved -> committed jumped straight past it.")
	return nil
}

// ---- helpers ----

func advance(ctx context.Context, cursorAdvancerDatastore *cursoradvancerdatastore.CursorAdvancerDatastore, topicId int64) int64 {
	c, err := cursorAdvancerDatastore.AdvanceCommitted(ctx, topicId, groupId)
	must(err)
	return c
}

func committedCol(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT committed FROM consumer_group_cursor_%d WHERE consumer_group_id=$1`, topicId), groupId)
}
func claimedCol(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT claimed FROM consumer_group_cursor_%d WHERE consumer_group_id=$1`, topicId), groupId)
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
