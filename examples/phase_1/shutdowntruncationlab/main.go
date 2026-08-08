package main

// Graceful-shutdown lease truncation lab.
//
// A shutdown signal mid-range must not force the WHOLE range to sit out a
// full lease-expiry reclaim: everything already resolved (successes + a
// parked exception) has to survive via closeOpenRanges' PartialCommit path,
// and only the untouched suffix should remain leased for a future reclaim.
//
// Claims one life straight off MessageConsumerDefinition (no manager) with pool
// N=1 so message dispatch is strictly serialized: consumerFunc cancels the
// shared context after message 2 finishes, simulating a shutdown signal
// arriving mid-range. Message 3 is already sitting in the buffer (prefetch
// claims the whole range of 3 up front) but N=1 means dispatch can't hand it
// out until message 2's permit releases -- by then ctx is already cancelled,
// so dispatch exits without ever calling WaitForNext for it.
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
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	vulkancommon "github.com/agentstax/vulkan/pkg/common"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	exceptionconsumercontroller "github.com/agentstax/vulkan/pkg/consumer/exceptionconsumer/controller"
	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer"
	messageconsumercontroller "github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/maintain"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/google/uuid"
)

const group = "phase9.shutdowntruncationlab"

// Timeout + QueueMargin + AckMargin below sums to this -- kept equal to the
// leaseDuration used for the manual reclaim call later, so both claims behave
// the same way.
const lease = 2 * time.Second

// set by main from RegisterGroup -- helpers are id-keyed
var groupID int64

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
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("phase9.shutdowntruncationlab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	cd, err := consumercontroller.NewConsumerController(ds, nil)
	must(err)
	messageConsumers, err := messageconsumercontroller.NewMessageConsumerController(ds, nil)
	must(err)
	exceptionConsumers, err := exceptionconsumercontroller.NewExceptionConsumerController(ds, nil)
	must(err)
	md, err := maintain.NewMaintenanceDatastore(ds, nil)
	must(err)
	wp, err := producer.NewProducer[common.Work](ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)

	groupID = mustGroupID(cd.RegisterGroup(ctx, tp.Id, group))
	seed(ctx, wpInstance, 3)

	cfg := &messageconsumer.MessageConsumerConfig{
		BatchLimit:         3,
		QueueSize:          10,
		MessageConcurrency: 1,
		Message:            &vulkancommon.MessageOptions{Timeout: 1 * time.Second},
		QueueMargin:        500 * time.Millisecond,
		AckMargin:          500 * time.Millisecond, // also PartialCommit's/ForceReclaimRange's own detached-ctx budget
	}
	owner, err := vulkancommon.NewConsumerGroupOwner(tp.SystemId, tp.Id, groupID, group)
	must(err)
	abandonedEvents, err := consumermetrics.NewMetricEventProducer(ds, &consumermetrics.MetricEventConfig{DisableGracefulShutdown: true})
	must(err)
	go func() { must(abandonedEvents.Run(ctx)) }()

	step("WORKER claims all 3, shutdown fires after message 2 -- message 3 never attempted")
	runCtx, cancel := context.WithCancel(ctx)
	var calls atomic.Int64
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		switch n := calls.Add(1); n {
		case 1:
			return nil // success
		case 2:
			cancel() // shutdown signal arrives -- this message still finishes though
			return errors.New("simulated failure")
		default:
			die(fmt.Sprintf("consumerFunc called a %dth time -- message 3 must never be attempted", n))
			return nil
		}
	}
	// claimed straight off the definition -- no manager, so nothing respawns the
	// execution and the truncation the lab asserts on is the only one
	definition, err := messageconsumer.NewMessageConsumerDefinition(ds, consumerFunc, abandonedEvents, cfg)
	must(err)
	must(definition.Declare(ctx, owner))

	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, definition.Name(), owner)
	must(err)
	execution, err := definition.Provision(runCtx, row.Id, &row.Owner, row.Metadata)
	must(err)

	// Run blocks until runCtx cancels (cancel() fires synchronously inside
	// consumerFunc above) -- N=1 pool means dispatch can't reach message 3
	// before that cancellation is already visible to it.
	if err := execution.Run(runCtx); err != nil && !errors.Is(err, context.Canceled) {
		die(fmt.Sprintf("Run returned an unexpected error: %v", err))
	}
	assert("exactly 2 messages attempted", calls.Load(), 2)

	lb := onlyLease(ctx, ds, tp.Id)
	fmt.Printf("  lease narrowed: (%d,%d] (was (0,%d])\n", lb.low, lb.high, lb.high)
	assert("lease survives (not deleted)", leases(ctx, ds, tp.Id), 1)
	assert("lease high unchanged", lb.high, 3)
	assert("lease low narrowed to message 2", lb.low, 2)
	assert("exactly 1 parked exception (message 2)", deliveries(ctx, ds, tp.Id), 1)
	assertStatus(ctx, ds, tp.Id, 2, "ready")

	step("waterline stays pinned behind the unresolved exception, even though the lease is already narrowed past it")
	committed := advance(ctx, md, tp.Id)
	assert("committed blocked at message 1 (exception at 2 still unresolved)", committed, 1)

	step("sleep 5.5s — let the parked exception's initial backoff pass")
	time.Sleep(5500 * time.Millisecond)

	step("resolve the exception -- waterline jumps to the narrowed low, no need to wait on the untouched suffix's lease")
	claimedExceptions, err := exceptionConsumers.ClaimExceptions(ctx, tp.Id, groupID, 10, 3, lease, tp.DisableDeliveryLog)
	must(err)
	if len(claimedExceptions) != 1 {
		die(fmt.Sprintf("expected 1 claimed exception, got %d", len(claimedExceptions)))
	}
	must(exceptionConsumers.RecordExceptionSuccess(ctx, &claimedExceptions[0], nil))
	committed = advance(ctx, md, tp.Id)
	assert("committed advances to the narrowed low", committed, 2)
	assert("deliveries drained (exception pop-deleted)", deliveries(ctx, ds, tp.Id), 0)

	// the narrowed lease's 2s duration already elapsed during the 5.5s backoff
	// sleep above -- no separate wait needed before reclaiming it.
	step("reclaim: only the untouched suffix comes back, not the resolved prefix")
	claim2, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupID, 3, 3, lease, false)
	must(err)
	if claim2 == nil {
		die("expected a reclaim, got nil")
	}
	assert("reclaimed range starts at the narrowed low", claim2.Lease.Low, 2)
	assert("reclaimed range ends at the original high", claim2.Lease.High, 3)
	assert("reclaimed exactly the untouched suffix (1 message)", int64(len(claim2.Messages)), 1)
	assert("reclaimed message is the one never attempted", claim2.Messages[0].Id, 3)

	must(messageConsumers.Commit(ctx, tp.Id, groupID, claim2.Lease.Token, nil, 5*time.Second, false))
	committed = advance(ctx, md, tp.Id)
	assert("committed reaches head", committed, 3)
	assert("no leases left open", leases(ctx, ds, tp.Id), 0)

	fmt.Println("\n✅ SHUTDOWN LEASE TRUNCATION LAB PASSED")
	fmt.Println("   an interruption mid-range parks what resolved and narrows the lease to the")
	fmt.Println("   untouched suffix -- the resolved prefix is never redelivered, the waterline's")
	fmt.Println("   exception-blocker and lease-narrowing terms combine correctly via LEAST, and")
	fmt.Println("   the untouched suffix reclaims on its own once its (now-shorter) lease expires.")
}

// ---- helpers ----

func seed(ctx context.Context, wpInstance *producer.ProducerInstance[common.Work], n int) {
	for range n {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
}

func advance(ctx context.Context, md *maintain.MaintenanceDatastore, topicID int64) int64 {
	c, err := md.AdvanceWaterline(ctx, topicID, groupID)
	must(err)
	return c
}

type leaseBounds struct{ low, high int64 }

func onlyLease(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) leaseBounds {
	var lb leaseBounds
	must(ds.Pool.QueryRow(ctx, `SELECT low, high FROM lease WHERE consumer_group_id=$1`, groupID).Scan(&lb.low, &lb.high))
	return lb
}

func leases(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, `SELECT count(*) FROM lease WHERE consumer_group_id=$1`, groupID)
}
func deliveries(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM delivery_%d WHERE consumer_group_id=$1`, topicID), groupID)
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func assertStatus(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, messageID int64, want string) {
	var got string
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT status FROM delivery_%d WHERE consumer_group_id=$1 AND message_id=$2`, topicID), groupID, messageID).Scan(&got))
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

func mustGroupID(g *consumercontroller.Group, err error) int64 { must(err); return g.Id }
