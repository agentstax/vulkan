package main

// CompactionRank lab: proves the (rank, id) winner rule live against a real
// database -- the claims Chunk 1/2 make about rank but can't verify without
// Postgres actually evaluating the row comparison.
//
// Registers its own topic (destroyed on exit), self-seeds, fully
// self-contained.
//
// Confirms, in order:
//   - a rank-pinned key ignores every normal-rank update that arrives after
//     it, even at a higher id -- the pin holds until something >= its rank
//     arrives.
//   - a -1 backfill write never beats a live write at the default rank 0,
//     regardless of which one arrives (and lands the higher id) first -- the
//     bridge's exact interleaving, proven both orderings.
//   - every losing row (pinned-over, backfilled) stays physically present in
//     message_log but never comes back on a claim.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	messageconsumercontroller "github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const group = "phase14a.compactionranklab"

// RankedRecord is this lab's own payload shape -- Label exists purely to make
// the printed claim output readable.
type RankedRecord struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

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

	topicName := fmt.Sprintf("phase14a.compactionranklab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	cd, err := consumercontroller.NewConsumerController(ds, nil)
	must(err)
	messageConsumers, err := messageconsumercontroller.NewMessageConsumerController(ds, nil)
	must(err)
	wp, err := producer.NewProducer[RankedRecord](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)
	groupID := mustGroupID(cd.RegisterGroup(ctx, tp.Id, group))

	const lease = 5 * time.Second
	const maxRangeReclaims = 3 // never exhausted in this lab -- no crashes/reclaims here

	// ===== pinning: a high rank ignores every normal-rank update after it (ids 1-4) =====
	step("user:1 gets a normal write, then a rank-100 pin, then two MORE normal writes")
	publish(ctx, wpInstance, "user:1", "v1-normal", 0) // id 1
	publish(ctx, wpInstance, "user:1", "v2-PIN", 100)  // id 2 <- pinned winner
	publish(ctx, wpInstance, "user:1", "v3-normal", 0) // id 3, higher id, still loses to the pin
	publish(ctx, wpInstance, "user:1", "v4-normal", 0) // id 4, higher id still, also loses

	assertInt("compaction_head still points at the pin despite two higher-id normal writes after it", headID(ctx, ds, tp.Id, "user:1"), 2)

	claim, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupID, 10, maxRangeReclaims, lease, false)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil")
	}
	assertIDs("only the pinned row comes back for user:1", ids(claim.Messages), []int64{2})
	must(messageConsumers.Commit(ctx, tp.Id, groupID, claim.Lease.Token, nil, 5*time.Second, false))
	assertInt("v1/v3/v4 still physically exist -- compaction filters, never deletes", rowCount(ctx, ds, tp.Id), 4)

	// ===== the bridge interleaving: -1 never beats 0, either arrival order (ids 5-8) =====
	step("live (rank 0) THEN backfill (rank -1) for user:2 -- backfill's higher id still loses")
	publish(ctx, wpInstance, "user:2", "live", 0)      // id 5 <- stays the winner
	publish(ctx, wpInstance, "user:2", "backfill", -1) // id 6, higher id, rank -1 loses to rank 0
	assertInt("compaction_head still points at the live write, not the higher-id backfill", headID(ctx, ds, tp.Id, "user:2"), 5)

	step("backfill (rank -1) THEN live (rank 0) for user:3 -- live wins as always")
	publish(ctx, wpInstance, "user:3", "backfill", -1) // id 7
	publish(ctx, wpInstance, "user:3", "live", 0)      // id 8 <- wins, same as any normal update would
	assertInt("compaction_head points at the live write", headID(ctx, ds, tp.Id, "user:3"), 8)

	claim, err = messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupID, 10, maxRangeReclaims, lease, false)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil")
	}
	assertIDs("only the two live-rank winners come back, neither backfill", ids(claim.Messages), []int64{5, 8})
	must(messageConsumers.Commit(ctx, tp.Id, groupID, claim.Lease.Token, nil, 5*time.Second, false))

	step("both backfill rows still physically exist, just never claimed")
	assertTrue("user:2's backfill row (id 6) still exists", rowExists(ctx, ds, tp.Id, 6))
	assertTrue("user:3's backfill row (id 7) still exists", rowExists(ctx, ds, tp.Id, 7))
	assertInt("all 8 rows still physically present", rowCount(ctx, ds, tp.Id), 8)

	fmt.Println("\n✅ COMPACTION RANK LAB PASSED")
	fmt.Println("   a pin holds against every normal-rank update after it -> a -1 backfill never")
	fmt.Println("   beats a 0 live write regardless of which arrives first -> every losing row stays")
	fmt.Println("   physically present, just never claimed.")
}

// ---- helpers ----

func publish(ctx context.Context, wpInstance *producer.ProducerInstance[RankedRecord], key, label string, rank int64) {
	_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*RankedRecord, error) {
		return &RankedRecord{Key: key, Label: label}, nil
	}, producer.ProduceOptions{CompactionKey: key, CompactionRank: rank})
	must(err)
}

func headID(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, key string) int64 {
	return scalar(ctx, ds, `SELECT head_id FROM compaction_head WHERE topic_id=$1 AND compaction_key=$2;`, topicID, key)
}

func rowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d`, topicID))
}

func rowExists(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, id int64) bool {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d WHERE id=$1`, topicID), id) == 1
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func ids(msgs []messageconsumercontroller.Message) []int64 {
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
func assertInt(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
func assertIDs(label string, got, want []int64) {
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
func assertTrue(label string, cond bool) {
	if !cond {
		die(fmt.Sprintf("%s: got false, want true", label))
	}
	fmt.Printf("  ✓ %s\n", label)
}

func mustGroupID(g *consumercontroller.Group, err error) int64 { must(err); return g.Id }
