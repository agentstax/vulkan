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
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	cursorGroup    = "phase7.cursor.lab"
	controlGroup   = "phase7.control.lab"
	lifecycleGroup = "phase7.lifecycle.lab"
)

// bindable is consumer.Datastore plus Bind/ClearBindings, which aren't part of
// that interface (they're admin operations, not something WorkConsumer itself
// calls) -- a small local interface is enough to accept the concrete (unexported)
// datastore struct as a helper param without naming its type.
type bindable interface {
	consumer.Datastore[common.Work]
	Bind(ctx context.Context, topicID int64, consumerGroup, pattern string) error
	ClearBindings(ctx context.Context, topicID int64, consumerGroup string) error
}

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("phase7.routinglab.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)
	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)

	head := reset(ctx, ds, cd, tp.Id, cursorGroup, controlGroup, lifecycleGroup)
	fmt.Printf("topic=%q id=%d message_log head = %d\n", topicName, tp.Id, head)

	// ===== publish msg1 BEFORE any binding exists =====
	step("publish msg1, no binding exists for any group yet")
	msg1 := publish(ctx, wp, "orders.us.created")
	fmt.Printf("  published %s\n", msg1)

	// ===== bind cursorGroup and lifecycleGroup, THEN publish the rest =====
	step("bind cursorGroup to orders.*.created, lifecycleGroup to payments.*")
	must(cd.Bind(ctx, tp.Id, cursorGroup, "orders.*.created"))
	must(cd.Bind(ctx, tp.Id, lifecycleGroup, "payments.*"))

	msg2 := publish(ctx, wp, "orders.us.central1.created") // deeper hierarchy, still matches (true wildcard)
	msg3 := publish(ctx, wp, "orders.eu.updated")          // wrong tail, does not match
	msg4 := publish(ctx, wp, "payments.charge")            // matches lifecycleGroup only
	msg5 := publish(ctx, wp, "")                           // NULL routing_key, matches nothing bound
	fmt.Printf("  published %s\n  published %s\n  published %s\n  published %s\n", msg2, msg3, msg4, msg5)

	const lease = 5 * time.Second
	const limit = 10
	const maxRangeReclaims = 3 // never hit in this lab -- no crashed/reclaimed ranges here

	// ===== CURSOR path: cursorGroup only sees the 2 matching messages =====
	step("cursorGroup claims (head, head+5] -- expect only msg1 and msg2 back")
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroup, limit, maxRangeReclaims, lease)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim.Lease.Low, claim.Lease.High, ids(claim.Messages))
	assertInt("range low is head", claim.Lease.Low, head)
	assertInt("range high covers all 5 published", claim.Lease.High, head+5)
	assertIDs("only msg1 (published before the binding existed) and msg2 (deeper hierarchy) match",
		ids(claim.Messages), []int64{head + 1, head + 2})

	must(cd.Commit(ctx, tp.Id, cursorGroup, claim.Lease.Token, nil, nil, 5*time.Second, false))
	committed := advance(ctx, cd, tp.Id, cursorGroup)
	assertInt("committed advances over the WHOLE range regardless of match", committed, head+5)

	// ===== CURSOR path: controlGroup has no binding, sees every message =====
	step("controlGroup claims the identical range -- expect all 5 back, unaffected by cursorGroup's binding")
	claim, err = cd.ClaimMessagesWithCursor(ctx, tp.Id, controlGroup, limit, maxRangeReclaims, lease)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim.Lease.Low, claim.Lease.High, ids(claim.Messages))
	assertIDs("an unbound group receives every message, including the NULL routing_key one",
		ids(claim.Messages), []int64{head + 1, head + 2, head + 3, head + 4, head + 5})

	must(cd.Commit(ctx, tp.Id, controlGroup, claim.Lease.Token, nil, nil, 5*time.Second, false))
	advance(ctx, cd, tp.Id, controlGroup)

	// ===== LIFECYCLE path: only a matching message ever gets a delivery row =====
	step("FanOut lifecycleGroup -- expect exactly 1 delivery row (msg4, payments.charge)")
	must(cd.FanOut(ctx, tp.Id, lifecycleGroup))
	deliveries, err := cd.ClaimMessagesWithLifecycle(ctx, tp.Id, lifecycleGroup, limit)
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

func publish(ctx context.Context, wp *producer.WorkProducer[common.Work], routingKey string) string {
	work, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, producer.ProduceOptions{RoutingKey: routingKey})
	must(err)
	return fmt.Sprintf("work=%s routing_key=%q", work.Id, routingKey)
}

// resets all three groups to a clean slate and fast-forwards their cursors to
// the current log head, so a fresh CURSOR claim only ever sees messages this
// lab itself publishes.
func reset(ctx context.Context, ds *coredatastore.PostgresDatastore, cd bindable, topicID int64, groups ...string) int64 {
	head := scalar(ctx, ds, fmt.Sprintf(`SELECT COALESCE(max(id),0) FROM message_log_%d`, topicID))
	for _, g := range groups {
		for _, q := range []string{
			`DELETE FROM lease WHERE consumer_group=$1 AND topic_id=$2`,
			`DELETE FROM cursor WHERE consumer_group=$1 AND topic_id=$2`,
		} {
			_, err := ds.Pool.Exec(ctx, q, g, topicID)
			must(err)
		}
		_, err := ds.Pool.Exec(ctx, fmt.Sprintf(`DELETE FROM delivery_%d WHERE consumer_group=$1`, topicID), g)
		must(err)
		must(cd.ClearBindings(ctx, topicID, g))
		must(cd.UpsertCursor(ctx, topicID, g))
		_, err = ds.Pool.Exec(ctx, `UPDATE cursor SET claimed=$3, committed=$3 WHERE consumer_group=$1 AND topic_id=$2`, g, topicID, head)
		must(err)
	}
	return head
}

func advance(ctx context.Context, cd consumer.Datastore[common.Work], topicID int64, group string) int64 {
	c, err := cd.AdvanceWaterline(ctx, topicID, group)
	must(err)
	return c
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

func deliveryIDs(rows []consumer.DeliveryRow) []int64 {
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
