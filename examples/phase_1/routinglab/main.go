package main

// routing lab: confirms bindings gate what a group receives, not what gets claimed.
//
// Drives the real datastore methods directly (BindTopic, ClaimMessagesWithCursor,
// FanOut, ClaimMessagesWithLifecycle) so matching is deterministic and asserted on
// exact returned rows, not inferred from timing. Publishes its own messages under
// a run-unique routing-key namespace so its assertions never collide with routing
// keys left behind by earlier runs of this or any other lab.
//
// Confirms: a binding added AFTER a message already exists still applies to it the
// next time it's read (the predicate runs at claim/fan-out time, not publish
// time); a true wildcard crosses hierarchy depth (`ns.orders.*.created` also
// matches `ns.orders.us.central1.created`); the CURSOR path excludes
// non-matching rows from what's returned but still advances committed over the
// whole range; the LIFECYCLE path excludes them from ever getting a deliveries
// row at all; and one group's binding has zero effect on another group reading
// the identical range.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/producer"
	prodstore "github.com/agentstax/vulkan/pkg/producer/datastore"
	"github.com/jackc/pgx/v5"
)

const (
	cursorGroup    = "phase7.cursor.lab"
	controlGroup   = "phase7.control.lab"
	lifecycleGroup = "phase7.lifecycle.lab"
)

func main() {
	ctx := context.Background()

	cd, err := consumer.NewPostgresDatastore[common.Work](ctx, &consumer.PostgresConnectionParams{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	pd, err := prodstore.NewPostgresDatastore[common.Work](ctx, &prodstore.PostgresConnectionParams{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	wp := producer.NewWorkProducer(pd)

	// every routing key this lab uses is namespaced under ns, so it can never
	// match a binding built from a prior run's leftover message_log rows.
	ns := fmt.Sprintf("routinglab%d", time.Now().UnixNano())

	head := reset(ctx, cd, cursorGroup, controlGroup, lifecycleGroup)
	fmt.Printf("message_log head = %d, namespace = %q\n", head, ns)

	// ===== publish msg1 BEFORE any binding exists =====
	step("publish msg1, no binding exists for any group yet")
	msg1 := publish(ctx, wp, ns+".orders.us.created")
	fmt.Printf("  published %s\n", msg1)

	// ===== bind cursorGroup and lifecycleGroup, THEN publish the rest =====
	step("bind cursorGroup to orders.*.created, lifecycleGroup to payments.*")
	must(cd.BindTopic(ctx, cursorGroup, ns+".orders.*.created"))
	must(cd.BindTopic(ctx, lifecycleGroup, ns+".payments.*"))

	msg2 := publish(ctx, wp, ns+".orders.us.central1.created") // deeper hierarchy, still matches (true wildcard)
	msg3 := publish(ctx, wp, ns+".orders.eu.updated")          // wrong tail, does not match
	msg4 := publish(ctx, wp, ns+".payments.charge")            // matches lifecycleGroup only
	msg5 := publish(ctx, wp, "")                               // NULL routing_key, matches nothing bound
	fmt.Printf("  published %s\n  published %s\n  published %s\n  published %s\n", msg2, msg3, msg4, msg5)

	const lease = 5 * time.Second
	const limit = 10
	const maxRangeReclaims = 3 // never hit in this lab -- no crashed/reclaimed ranges here

	// ===== CURSOR path: cursorGroup only sees the 2 matching messages =====
	step("cursorGroup claims (head, head+5] -- expect only msg1 and msg2 back")
	claim, err := cd.ClaimMessagesWithCursor(ctx, cursorGroup, limit, maxRangeReclaims, lease)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim.Lease.Low, claim.Lease.High, ids(claim.Messages))
	assertInt("range low is head", claim.Lease.Low, head)
	assertInt("range high covers all 5 published", claim.Lease.High, head+5)
	assertIDs("only msg1 (published before the binding existed) and msg2 (deeper hierarchy) match",
		ids(claim.Messages), []int64{head + 1, head + 2})

	must(cd.Commit(ctx, cursorGroup, claim.Lease.Token, nil, nil))
	committed := advance(ctx, cd, cursorGroup)
	assertInt("committed advances over the WHOLE range regardless of match", committed, head+5)

	// ===== CURSOR path: controlGroup has no binding, sees every message =====
	step("controlGroup claims the identical range -- expect all 5 back, unaffected by cursorGroup's binding")
	claim, err = cd.ClaimMessagesWithCursor(ctx, controlGroup, limit, maxRangeReclaims, lease)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim.Lease.Low, claim.Lease.High, ids(claim.Messages))
	assertIDs("an unbound group receives every message, including the NULL routing_key one",
		ids(claim.Messages), []int64{head + 1, head + 2, head + 3, head + 4, head + 5})

	must(cd.Commit(ctx, controlGroup, claim.Lease.Token, nil, nil))
	advance(ctx, cd, controlGroup)

	// ===== LIFECYCLE path: only a matching message ever gets a deliveries row =====
	step("FanOut lifecycleGroup -- expect exactly 1 deliveries row (msg4, payments.charge)")
	must(cd.FanOut(ctx, lifecycleGroup))
	deliveries, err := cd.ClaimMessagesWithLifecycle(ctx, lifecycleGroup, limit)
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
	work, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, routingKey)
	must(err)
	return fmt.Sprintf("work=%s routing_key=%q", work.Id, routingKey)
}

// resets all three groups to a clean slate and fast-forwards their cursors to
// the current message_log head, so a fresh CURSOR claim only ever sees messages
// this lab itself publishes -- not whatever accumulated from earlier runs.
func reset(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], groups ...string) int64 {
	head := scalar(ctx, cd, `SELECT COALESCE(max(id),0) FROM message_log`)
	for _, g := range groups {
		for _, q := range []string{
			`DELETE FROM leases WHERE consumer_group=$1`,
			`DELETE FROM deliveries WHERE consumer_group=$1`,
			`DELETE FROM cursors WHERE consumer_group=$1`,
		} {
			_, err := cd.Pool.Exec(ctx, q, g)
			must(err)
		}
		must(cd.ClearBindings(ctx, g))
		must(cd.UpsertCursor(ctx, g))
		_, err := cd.Pool.Exec(ctx, `UPDATE cursors SET claimed=$2, committed=$2 WHERE consumer_group=$1`, g, head)
		must(err)
	}
	return head
}

func advance(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], group string) int64 {
	c, err := cd.AdvanceWaterline(ctx, group)
	must(err)
	return c
}

func scalar(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], q string, args ...any) int64 {
	var v int64
	must(cd.Pool.QueryRow(ctx, q, args...).Scan(&v))
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
