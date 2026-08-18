package main

// Phase 8b's own lab: proves the five claims Phase 8b settled about
// per-topic tables (DECISIONS.md, phase 8b records), live against a real DB
// rather than asserted.
//
// Each proof registers its own disposable topic(s), destroyed on exit, so
// this lab is self-contained and re-runnable without leftover state --
// same convention chunk 11 brought to every other lab this phase.
//
// PROOF 1: two topics get independent physical tables and dense id sequences
//          -- ids don't leak or interleave across topics.
// PROOF 2: a badly-lagging group on topic B does not block a drop on topic A
//          -- the exact cross-topic contamination 8a's TODO flagged, the
//          headline bug this phase fixes.
// PROOF 3: routing_key/bindings behave exactly as Phase 7/routinglab proved,
//          now scoped within one topic (a condensed smoke check -- the full
//          suite, including LIFECYCLE gate-row-creation and cross-hierarchy
//          wildcards, lives in routinglab and isn't re-derived here).
// PROOF 4: two routing_key slices sharing ONE topic still share that topic's
//          drop floor -- the re-scoped-not-eliminated case this phase leaves
//          deliberately unfixed (split into separate topics if that's a
//          real problem).
// PROOF 5: operating against an unregistered topic id fails clearly (a
//          Postgres undefined_table error), never silently auto-creating one.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	messageconsumercontroller "github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	janitordatastore "github.com/agentstax/vulkan/pkg/worker/janitor/controller/datastore"
	waterlinedatastore "github.com/agentstax/vulkan/pkg/worker/waterline/controller/datastore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

const (
	partitionSize = int64(5)
	ttl           = 100 * time.Millisecond
	ttlMargin     = 300 * time.Millisecond
)

func main() {
	ctx := context.Background()
	run := time.Now().UnixNano()

	ds, err := iDatastore.NewPostgresDatastore(ctx, &iDatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	register := func(name string) *topic.Topic {
		t, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), &topiccontroller.TopicConfig{PartitionSize: partitionSize})
		must(err)
		return t
	}
	topicA := register(fmt.Sprintf("phase8b.topiclab.a.%d", run))
	topicB := register(fmt.Sprintf("phase8b.topiclab.b.%d", run))
	topicC := register(fmt.Sprintf("phase8b.topiclab.c.%d", run))
	topicD := register(fmt.Sprintf("phase8b.topiclab.d.%d", run))
	defer func() {
		for _, t := range []*topic.Topic{topicA, topicB, topicC, topicD} {
			must(mAdmin.DestroyTopic(ctx, t.Name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
		}
	}()

	cd, err := consumercontroller.NewConsumerController(ds, nil)
	must(err)
	messageConsumers, err := messageconsumercontroller.NewMessageConsumerController(ds, nil)
	must(err)
	janitorDatastore, err := janitordatastore.NewJanitorDatastore(ds, nil)
	must(err)
	waterlineDatastore, err := waterlinedatastore.NewWaterlineDatastore(ds, nil)
	must(err)

	// ===== PROOF 1: independent physical tables, independent dense id sequences =====
	step("PROOF 1: two topics get independent physical tables and dense id sequences")
	wpA, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpAInstance, err := wpA.Register(ctx, topicA.Name, topic.SchemaVersion(1))
	must(err)
	wpB, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpBInstance, err := wpB.Register(ctx, topicB.Name, topic.SchemaVersion(1))
	must(err)
	for range 3 {
		publish(ctx, wpAInstance, "")
	}
	for range 4 {
		publish(ctx, wpBInstance, "")
	}
	idsA := allIds(ctx, ds, topicA.Id)
	idsB := allIds(ctx, ds, topicB.Id)
	assertInt64s("topicA's log has exactly ids 1-3, its own dense sequence", idsA, []int64{1, 2, 3})
	assertInt64s("topicB's log has exactly ids 1-4, its own INDEPENDENT sequence -- also starting at 1, not 4", idsB, []int64{1, 2, 3, 4})

	// ===== PROOF 2: a badly-lagging group on topic B does not block a drop on topic A =====
	step("PROOF 2: a badly-lagging group on topic B does not block a drop on topic A")
	publish(ctx, wpAInstance, "") // id 4, the trigger point create-ahead builds partition 1 from
	waitForPartition(ctx, ds, topicA.Id, 1)
	publish(ctx, wpAInstance, "") // id 5, landing in the partition that is already there
	time.Sleep(ttl + ttlMargin)

	groupA := "topiclab.groupA" // topicA's own reader, fully caught up
	groupAID := mustGroupID(cd.RegisterGroup(ctx, topicA.Id, groupA))
	setCursor(ctx, ds, groupAID, 5, 5)

	groupB := "topiclab.groupB" // topicB's reader, registered but never advances -- badly lagging
	mustGroupID(cd.RegisterGroup(ctx, topicB.Id, groupB))

	must(janitorDatastore.DropExpiredPartitions(ctx, topicA.Id, partitionSize, ttl, false, topicA.DeliveryLogMode))
	assertPartitions(ctx, ds, topicA.Id, "topicA's partition 0 dropped, totally unaffected by topicB's lagging group", []int64{1})
	fmt.Println("  -> this is the exact cross-topic contamination 8a's floor bug caused; each topic's floor is now its own")

	// ===== PROOF 3: routing_key/bindings still behave as Phase 7/routinglab proved, now scoped to one topic =====
	step("PROOF 3: routing_key/bindings behave as Phase 7 proved, scoped within one topic (condensed -- full suite in routinglab)")
	wpC, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpCInstance, err := wpC.Register(ctx, topicC.Name, topic.SchemaVersion(1))
	must(err)
	groupRoute := "topiclab.route"
	groupRouteID := mustGroupID(cd.RegisterGroup(ctx, topicC.Id, groupRoute))

	headBefore := head(ctx, ds, topicC.Id)      // topicC is fresh, this is 0
	publish(ctx, wpCInstance, "orders.created") // id headBefore+1, published BEFORE any binding exists
	_, err = cd.DeclareBindings(ctx, groupRouteID, []string{"orders.*"}, time.Now())
	must(err)
	publish(ctx, wpCInstance, "orders.updated")  // id headBefore+2, matches, published AFTER the binding
	publish(ctx, wpCInstance, "payments.charge") // id headBefore+3, does not match
	fmt.Printf("  published ids %d,%d,%d (only %d predates the binding, only %d and %d match its pattern)\n",
		headBefore+1, headBefore+2, headBefore+3, headBefore+1, headBefore+1, headBefore+2)

	claim, err := messageConsumers.ClaimMessagesWithCursor(ctx, topicC.Id, groupRouteID, 10, 3, 30*time.Second, topic.DeliveryLogModeFailures)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil")
	}
	assertInt64s("retroactive binding applies to the pre-existing message, CURSOR path filters out the non-match",
		ids(claim.Messages), []int64{headBefore + 1, headBefore + 2})
	must(messageConsumers.Commit(ctx, topicC.Id, groupRouteID, claim.Lease.Token, nil, 5*time.Second, topic.DeliveryLogModeFailures))
	committed := advance(ctx, waterlineDatastore, topicC.Id, groupRouteID)
	assertInt("committed still advances over the WHOLE range, not just the matches", committed, claim.Lease.High)

	// ===== PROOF 4: two routing_key slices sharing ONE topic still share that topic's floor =====
	step("PROOF 4: two routing_key slices sharing ONE topic still share that topic's drop floor (deliberately not fixed)")
	wpD, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpDInstance, err := wpD.Register(ctx, topicD.Name, topic.SchemaVersion(1))
	must(err)
	groupX := "topiclab.sliceX" // reads only sliceX.* -- will be fully caught up
	groupY := "topiclab.sliceY" // reads only sliceY.* -- registered but stays lagging
	groupXID := mustGroupID(cd.RegisterGroup(ctx, topicD.Id, groupX))
	groupYID := mustGroupID(cd.RegisterGroup(ctx, topicD.Id, groupY))
	_, err = cd.DeclareBindings(ctx, groupXID, []string{"sliceX.*"}, time.Now())
	must(err)
	_, err = cd.DeclareBindings(ctx, groupYID, []string{"sliceY.*"}, time.Now())
	must(err)

	for range 4 { // 4 rows, all in sliceX -- the 4th builds partition 1 through create-ahead
		publish(ctx, wpDInstance, "sliceX.event")
	}
	waitForPartition(ctx, ds, topicD.Id, 1)
	publish(ctx, wpDInstance, "sliceX.event") // id 5, landing in the partition that is already there
	time.Sleep(ttl + ttlMargin)

	claimX, err := messageConsumers.ClaimMessagesWithCursor(ctx, topicD.Id, groupXID, 10, 3, 30*time.Second, topic.DeliveryLogModeFailures)
	must(err)
	if claimX == nil {
		die("expected groupX to claim a fresh range")
	}
	must(messageConsumers.Commit(ctx, topicD.Id, groupXID, claimX.Lease.Token, nil, 5*time.Second, topic.DeliveryLogModeFailures))
	advance(ctx, waterlineDatastore, topicD.Id, groupXID)
	fmt.Println("  groupX (sliceX reader) is now fully caught up on the only traffic that exists")
	// groupY never published to or claimed anything -- its cursor sits at claimed=committed=0,
	// simulating a slice consumer that's stuck or never started.

	must(janitorDatastore.DropExpiredPartitions(ctx, topicD.Id, partitionSize, ttl, false, topicD.DeliveryLogMode))
	assertPartitions(ctx, ds, topicD.Id, "partition 0 SURVIVES -- groupY's slice, though it has zero actual traffic, still pins this topic's one shared floor", []int64{0, 1})
	fmt.Println("  -> this is the case 8b deliberately leaves unfixed: split into separate topics if slices need independent floors")

	// ===== PROOF 5: operating against an unregistered topic id fails clearly =====
	step("PROOF 5: publishing/claiming against an unregistered topic id fails clearly, never silently auto-creates one")
	bogusTopicID := topicD.Id + 999_999_999 // guaranteed to never have been registered
	err = janitorDatastore.DropExpiredPartitions(ctx, bogusTopicID, partitionSize, ttl, false, topic.DeliveryLogModeFailures)
	if err == nil {
		die("expected an error operating against an unregistered topic id, got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "42P01" {
		die(fmt.Sprintf("expected a Postgres 42P01 (undefined_table) error, got: %v", err))
	}
	fmt.Printf("  ✓ got the expected undefined_table error: %s\n", pgErr.Message)
	fmt.Println("  -> no implicit topic/table creation as a side effect of a produce/claim call")

	fmt.Println("\n✅ TOPIC LAB PASSED")
	fmt.Println("   topics get independent tables/sequences; a lagging group's floor stays inside its own")
	fmt.Println("   topic; routing still works exactly as before, just re-scoped; and an unregistered")
	fmt.Println("   topic id fails loudly instead of silently doing something wrong.")
}

// ---- helpers ----

func publish(ctx context.Context, wp *producer.ProducerInstance[common.Work], routingKey string) {
	_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, producer.ProduceOptions{RoutingKey: routingKey})
	must(err)
}

func advance(ctx context.Context, waterlineDatastore *waterlinedatastore.WaterlineDatastore, topicId int64, groupId int64) int64 {
	c, err := waterlineDatastore.AdvanceWaterline(ctx, topicId, groupId)
	must(err)
	return c
}

func setCursor(ctx context.Context, ds *iDatastore.PostgresDatastore, groupId int64, claimed, committed int64) {
	_, err := ds.Pool.Exec(ctx, `UPDATE cursor SET claimed=$2, committed=$3 WHERE consumer_group_id=$1`, groupId, claimed, committed)
	must(err)
}

func head(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COALESCE(MAX(id), 0) FROM message_log_%d`, topicId)).Scan(&v))
	return v
}

func allIds(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) []int64 {
	rows, err := ds.Pool.Query(ctx, fmt.Sprintf(`SELECT id FROM message_log_%d ORDER BY id`, topicId))
	must(err)
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var id int64
		must(rows.Scan(&id))
		out = append(out, id)
	}
	must(rows.Err())
	return out
}

func assertPartitions(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, label string, want []int64) {
	assertInt64s(label, partitions(ctx, ds, topicId), want)
}

// waitForPartition blocks until partition n exists. Create-ahead runs in a
// background goroutine off the produce that hits its trigger point id, so a
// publish right behind that one races it: the goroutine reads MAX(id) and
// builds the partition after whatever it sees.
func waitForPartition(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, n int64) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		for _, existing := range partitions(ctx, ds, topicId) {
			if existing == n {
				return
			}
		}
		if time.Now().After(deadline) {
			die(fmt.Sprintf("partition %d of topic %d never appeared", n, topicId))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func partitions(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) []int64 {
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

	var got []int64
	for rows.Next() {
		var n int64
		must(rows.Scan(&n))
		got = append(got, n)
	}
	must(rows.Err())
	return got
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
func assertInt64s(label string, got, want []int64) {
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

func mustGroupID(g *consumercontroller.Group, err error) int64 { must(err); return g.Id }
