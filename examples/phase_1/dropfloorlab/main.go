package main

// Phase 8a lab (b): partition-drop is a hole in the log, and the cursor
// committed floor decides whether a lagging group is allowed to fall into it.
//
// Registers its own topic with a lab-scale PartitionSize (5 rows), destroyed on
// exit -- under 8b, partition width is a per-topic Register() param, so this lab
// no longer needs the pre-8b schema-swap hack partitionlab's own comment used to
// describe (DROP+recreate the shared message_log table, permanently discarding
// its rows). A dedicated topic gets its own message_log_<id> at exactly the
// width this lab wants, and its own cursorFloor -- so this lab's groups can't be
// blocked by, or block, any other lab's leftover state either, another thing the
// pre-8b version had to work around.
//
// Confirms three things about DropExpiredPartitions:
//  1. a group already past a partition's range is unaffected by its drop --
//     its next claim just reads on from where it was, from a partition that
//     never went anywhere.
//  2. a group whose cursor sits inside the soon-to-be-dropped range claims an
//     EMPTY batch once it's gone, yet still advances its `claimed` frontier
//     past the hole -- FreshClaimMessagesWithCursor computes the next range
//     from id arithmetic against MAX(id), never from what rows still exist.
//  3. the drop floor (MIN(committed) across cursors, now scoped to this topic)
//     refuses to drop a partition a lagging group hasn't committed past yet,
//     and AllowDropPastCommitted is the explicit override that waives it.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	messageconsumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	janitordatastore "github.com/agentstax/vulkan/pkg/topic/janitor/controller/datastore"
	"github.com/google/uuid"
)

const (
	partitionSize = int64(5)
	ttl           = 100 * time.Millisecond
	ttlMargin     = 300 * time.Millisecond // sleep past ttl with generous headroom, same convention as reclaimlab/exceptionlab's backoff waits

	groupPast   = "phase8a.drop.past"   // already committed past the range about to be dropped
	groupInside = "phase8a.drop.inside" // cursor still sits inside it
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

	topicName := fmt.Sprintf("phase8a.dropfloorlab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{PartitionSize: partitionSize})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	cd, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	messageConsumers, err := messageconsumergroupcontroller.NewMessageConsumerGroupController(ds, nil)
	must(err)
	janitorDatastore, err := janitordatastore.NewJanitorDatastore(ds, nil)
	must(err)
	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)

	step("publish ids 1-4 into message_log_<id>_0, then let them age past ttl")
	for range 4 {
		publish(ctx, wpInstance)
	}
	time.Sleep(ttl + ttlMargin)

	step("publish ids 5-9 into message_log_<id>_1 -- fresh, rolls the active partition forward")
	createPartition(ctx, ds, tp.Id, 1)
	for range 5 {
		publish(ctx, wpInstance)
	}
	createPartition(ctx, ds, tp.Id, 2)
	assertPartitions("partitions 0/1/2 exist (2 is pre-created headroom)", partitionNumbers(ctx, ds, tp.Id), []int64{0, 1, 2})

	step("groupPast fast-forwards past partition 0's range (claimed=committed=4) before the drop")
	reset(ctx, cd, ds, tp.Id, groupPast)
	setCursor(ctx, ds, tp.Id, groupPast, 4, 4)

	step("drop -- floor sits at groupPast's committed=4, exactly partition 0's last id, so it's not blocked")
	must(janitorDatastore.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, false, tp.DeliveryLogMode))
	assertPartitions("partition 0 dropped", partitionNumbers(ctx, ds, tp.Id), []int64{1, 2})

	step("groupPast claims on -- unaffected by the drop, reads real messages from partition 1")
	claim := freshClaim(ctx, messageConsumers, tp.Id, groupPast, 5)
	assertInt("claimed advances 4 -> 9", claim.Lease.High, 9)
	assertInt("5 real messages came back", int64(len(claim.Messages)), 5)

	step("groupInside starts fresh (claimed=committed=0) -- its position now sits inside the dropped range")
	reset(ctx, cd, ds, tp.Id, groupInside)

	step("groupInside claims (0,4] -- exactly the dropped range")
	claim = freshClaim(ctx, messageConsumers, tp.Id, groupInside, 4)
	assertInt("claimed still advances 0 -> 4 (id arithmetic, not row existence)", claim.Lease.High, 4)
	assertInt("but the batch is empty -- those rows are gone", int64(len(claim.Messages)), 0)
	fmt.Println("  -> a dropped partition is a hole a lagging cursor walks straight over, not a stall")

	step("publish ids 10-14 into partition 2, then let partition 1's rows (5-9) age past ttl")
	time.Sleep(ttl + ttlMargin)
	for range 5 {
		publish(ctx, wpInstance)
	}
	createPartition(ctx, ds, tp.Id, 3)
	assertPartitions("partitions 1/2/3 exist (3 is pre-created headroom)", partitionNumbers(ctx, ds, tp.Id), []int64{1, 2, 3})

	step("drop attempt -- groupInside's committed is still 0, floor blocks partition 1 (last id 9 > floor 0)")
	must(janitorDatastore.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, false, tp.DeliveryLogMode))
	assertPartitions("partition 1 survives -- refused by the floor", partitionNumbers(ctx, ds, tp.Id), []int64{1, 2, 3})

	step("same drop, AllowDropPastCommitted=true -- the floor check is waived outright")
	must(janitorDatastore.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, tp.DeliveryLogMode))
	assertPartitions("partition 1 dropped once the floor is overridden", partitionNumbers(ctx, ds, tp.Id), []int64{2, 3})

	fmt.Println("\n✅ DROP FLOOR LAB PASSED")
	fmt.Println("   a group past the drop is unaffected, a lagging cursor claims empty and advances")
	fmt.Println("   over the hole, and the floor refuses a drop until it's committed past it or waived.")
	return nil
}

// ---- helpers ----

func publish(ctx context.Context, wpInstance *producer.ProducerInstance[common.Work]) {
	_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, producer.ProduceOptions{})
	must(err)
}

// createPartition pre-creates message_log_<topicId>_<n> so the lab's ids stay
// dense -- production creates partitions on the produce path's self-heal,
// which burns an id per boundary and would shift every id asserted below.
func createPartition(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, n int64) {
	_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS message_log_%d_%d
			PARTITION OF message_log_%d
			FOR VALUES FROM (%d) TO (%d);
	`, topicId, n, topicId, n*partitionSize, (n+1)*partitionSize))
	must(err)
}

func reset(ctx context.Context, cd *consumergroupcontroller.ConsumerGroupController, ds *iDatastore.PostgresDatastore, topicId int64, group string) {
	groupId = mustGroupID(cd.RegisterGroup(ctx, topicId, group))
	_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`DELETE FROM claim_lease_%d WHERE consumer_group_id=$1`, topicId), groupId)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`DELETE FROM exception_queue_%d WHERE consumer_group_id=$1`, topicId), groupId)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`UPDATE consumer_group_cursor_%d SET claimed=0, committed=0, settled_head=0, pending_head=0, pending_xmax=NULL WHERE consumer_group_id=$1`, topicId), groupId)
	must(err)
}

func setCursor(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, group string, claimed, committed int64) {
	_ = group // groups are id-keyed; the name stays in the signature for the call sites' readability
	_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`UPDATE consumer_group_cursor_%d SET claimed=$2, committed=$3 WHERE consumer_group_id=$1`, topicId), groupId, claimed, committed)
	must(err)
}

func freshClaim(ctx context.Context, cd *messageconsumergroupcontroller.MessageConsumerGroupController, topicId int64, group string, limit int) *messageconsumergroupcontroller.ClaimedRange {
	claim, err := cd.ClaimMessagesWithCursor(ctx, topicId, groupId, limit, 3, 30*time.Second, topic.DeliveryLogModeFailures)
	must(err)
	if claim == nil {
		die(fmt.Sprintf("%s: expected a claim, got nil (already caught up?)", group))
	}
	return claim
}

func partitionNumbers(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) []int64 {
	prefix := fmt.Sprintf("message_log_%d_", topicId)
	rows, err := ds.Pool.Query(ctx, `
		SELECT REPLACE(c.relname, $2, '')::bigint AS n
		FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = $1::regclass
			AND c.relname LIKE $2 || '%'
		ORDER BY n;
	`, fmt.Sprintf("message_log_%d", topicId), prefix)
	must(err)
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var n int64
		must(rows.Scan(&n))
		out = append(out, n)
	}
	must(rows.Err())
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
func assertInt(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
func assertPartitions(label string, got, want []int64) {
	if len(got) != len(want) {
		die(fmt.Sprintf("%s: got %v, want %v", label, got, want))
	}
	for i := range got {
		if got[i] != want[i] {
			die(fmt.Sprintf("%s: got %v, want %v", label, got, want))
		}
	}
	fmt.Printf("  ✓ %s %v\n", label, got)
}

func mustGroupID(g *consumergroup.Group, err error) int64 { must(err); return g.Id }
