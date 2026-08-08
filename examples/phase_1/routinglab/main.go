package main

// routing lab: confirms bindings gate what a group receives, not what gets claimed.
//
// Registers its own topic, destroyed on exit, so every run starts from a
// genuinely empty log -- no routing-key namespacing needed to dodge leftover
// rows from earlier runs (a trick the pre-8b shared-message_log version needed
// and this one doesn't).
//
// Drives the real datastore methods directly (Bind, ClaimMessagesWithCursor,
// FanOut, ClaimMessagesWithLifecycle) so matching is deterministic and asserted on
// exact returned rows, not inferred from timing.
//
// Confirms: a binding added AFTER a message already exists still applies to it the
// next time it's read (the predicate runs at claim/fan-out time, not publish
// time); a true wildcard crosses hierarchy depth (`orders.*.created` also
// matches `orders.us.central1.created`); the CURSOR path excludes
// non-matching rows from what's returned but still advances committed over the
// whole range; the LIFECYCLE path excludes them from ever getting a delivery
// row at all; and one group's binding has zero effect on another group reading
// the identical range.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	deliveryconsumercontroller "github.com/agentstax/vulkan/pkg/consumer/deliveryconsumer/controller"
	messageconsumercontroller "github.com/agentstax/vulkan/pkg/consumer/messageconsumer/controller"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/maintain"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const (
	cursorGroup    = "phase7.cursor.lab"
	controlGroup   = "phase7.control.lab"
	lifecycleGroup = "phase7.lifecycle.lab"
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
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("phase7.routinglab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	cd, err := consumercontroller.NewConsumerController(ds, nil)
	must(err)
	messageConsumers, err := messageconsumercontroller.NewMessageConsumerController(ds, nil)
	must(err)
	deliveryConsumers, err := deliveryconsumercontroller.NewDeliveryConsumerController(ds, nil)
	must(err)
	md, err := maintain.NewMaintenanceDatastore(ds, nil)
	must(err)
	wp, err := producer.NewProducer[common.Work](ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)

	head, gids := reset(ctx, ds, cd, tp.Id, cursorGroup, controlGroup, lifecycleGroup)
	cursorGroupID, controlGroupID, lifecycleGroupID := gids[cursorGroup], gids[controlGroup], gids[lifecycleGroup]
	fmt.Printf("topic=%q id=%d message_log head = %d\n", topicName, tp.Id, head)

	// ===== publish msg1 BEFORE any binding exists =====
	step("publish msg1, no binding exists for any group yet")
	msg1 := publish(ctx, wpInstance, "orders.us.created")
	fmt.Printf("  published %s\n", msg1)

	// ===== bind cursorGroup and lifecycleGroup, THEN publish the rest =====
	step("bind cursorGroup to orders.*.created, lifecycleGroup to payments.*")
	must(cd.Bind(ctx, cursorGroupID, "orders.*.created"))
	must(cd.Bind(ctx, lifecycleGroupID, "payments.*"))

	msg2 := publish(ctx, wpInstance, "orders.us.central1.created") // deeper hierarchy, still matches (true wildcard)
	msg3 := publish(ctx, wpInstance, "orders.eu.updated")          // wrong tail, does not match
	msg4 := publish(ctx, wpInstance, "payments.charge")            // matches lifecycleGroup only
	msg5 := publish(ctx, wpInstance, "")                           // NULL routing_key, matches nothing bound
	fmt.Printf("  published %s\n  published %s\n  published %s\n  published %s\n", msg2, msg3, msg4, msg5)

	const lease = 5 * time.Second
	const limit = 10
	const maxRangeReclaims = 3 // never hit in this lab -- no crashed/reclaimed ranges here

	// ===== CURSOR path: cursorGroup only sees the 2 matching messages =====
	step("cursorGroup claims (head, head+5] -- expect only msg1 and msg2 back")
	claim, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroupID, limit, maxRangeReclaims, lease, false)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim.Lease.Low, claim.Lease.High, ids(claim.Messages))
	assertInt("range low is head", claim.Lease.Low, head)
	assertInt("range high covers all 5 published", claim.Lease.High, head+5)
	assertIDs("only msg1 (published before the binding existed) and msg2 (deeper hierarchy) match",
		ids(claim.Messages), []int64{head + 1, head + 2})

	must(messageConsumers.Commit(ctx, tp.Id, cursorGroupID, claim.Lease.Token, nil, 5*time.Second, false))
	committed := advance(ctx, md, tp.Id, cursorGroupID)
	assertInt("committed advances over the WHOLE range regardless of match", committed, head+5)

	// ===== CURSOR path: controlGroup has no binding, sees every message =====
	step("controlGroup claims the identical range -- expect all 5 back, unaffected by cursorGroup's binding")
	claim, err = messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, controlGroupID, limit, maxRangeReclaims, lease, false)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim.Lease.Low, claim.Lease.High, ids(claim.Messages))
	assertIDs("an unbound group receives every message, including the NULL routing_key one",
		ids(claim.Messages), []int64{head + 1, head + 2, head + 3, head + 4, head + 5})

	must(messageConsumers.Commit(ctx, tp.Id, controlGroupID, claim.Lease.Token, nil, 5*time.Second, false))
	advance(ctx, md, tp.Id, controlGroupID)

	// ===== LIFECYCLE path: only a matching message ever gets a delivery row =====
	step("FanOut lifecycleGroup -- expect exactly 1 delivery row (msg4, payments.charge)")
	must(deliveryConsumers.FanOut(ctx, tp.Id, lifecycleGroupID, 100))
	deliveries, err := deliveryConsumers.ClaimMessagesWithLifecycle(ctx, tp.Id, lifecycleGroupID, limit)
	must(err)
	fmt.Printf("  claimed deliveries: %v\n", deliveryIDs(deliveries))
	assertIDs("payments.charge is the only message materialized as a delivery",
		deliveryIDs(deliveries), []int64{head + 4})

	fmt.Println("\n✅ ROUTING LAB PASSED")
	fmt.Println("   binding predicate applies at claim/fan-out time, not publish time -> true wildcard")
	fmt.Println("   crosses hierarchy depth -> CURSOR path filters what's returned but still advances the")
	fmt.Println("   full range -> LIFECYCLE path never materializes a row for a non-match at all.")
}

// ---- helpers ----

func publish(ctx context.Context, wpInstance *producer.ProducerInstance[common.Work], routingKey string) string {
	work, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, producer.ProduceOptions{RoutingKey: routingKey})
	must(err)
	return fmt.Sprintf("work=%s routing_key=%q", work.Id, routingKey)
}

// resets all three groups to a clean slate and fast-forwards their cursors to
// the current log head, so a fresh CURSOR claim only ever sees messages this
// lab itself publishes.
func reset(ctx context.Context, ds *coredatastore.PostgresDatastore, cd *consumercontroller.ConsumerController, topicID int64, groups ...string) (int64, map[string]int64) {
	head := scalar(ctx, ds, fmt.Sprintf(`SELECT COALESCE(max(id),0) FROM message_log_%d`, topicID))
	gids := map[string]int64{}
	for _, g := range groups {
		gID := mustGroupID(cd.RegisterGroup(ctx, topicID, g))
		gids[g] = gID
		_, err := ds.Pool.Exec(ctx, `DELETE FROM lease WHERE consumer_group_id=$1`, gID)
		must(err)
		_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`DELETE FROM delivery_%d WHERE consumer_group_id=$1`, topicID), gID)
		must(err)
		must(cd.ClearBindings(ctx, gID))
		// settled/pending must ride along -- the claim gate assumes
		// gate >= settled >= claimed; bumping claimed alone breaks that and a
		// poll where the fresh pair doesn't prove would regress the cursor
		_, err = ds.Pool.Exec(ctx, `UPDATE cursor SET claimed=$2, committed=$2, settled_head=$2, pending_head=$2, pending_xmax=NULL WHERE consumer_group_id=$1`, gID, head)
		must(err)
	}
	return head, gids
}

func advance(ctx context.Context, md *maintain.MaintenanceDatastore, topicID int64, groupID int64) int64 {
	c, err := md.AdvanceWaterline(ctx, topicID, groupID)
	must(err)
	return c
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

func deliveryIDs(rows []deliveryconsumercontroller.Delivery) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.MessageId
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

func mustGroupID(g *consumercontroller.Group, err error) int64 { must(err); return g.Id }
