package main

// DeleteTopic cascade lab: confirms Destroy doesn't just drop message_log and
// the topic row -- it also has to clean up every other table scoped by
// topic_id (cursor, lease, binding, latest_key) and drop the per-topic
// delivery_<id>/delivery_log_<id>/idempotency_key_<id> tables outright, or
// that state is permanently orphaned (nothing else ever deletes it).
//
// Seeds one row in each of the shared tables plus the per-topic delivery and
// idempotency_key tables via the real datastore methods, deliberately
// leaving a lease OPEN and a delivery row unclaimed -- the messiest state a
// topic could be destroyed in mid-flight, not a conveniently-already-resolved
// one. Also records one failed lifecycle attempt so delivery_log_<id> is
// exercised and confirmed dropped outright, same as delivery_<id>.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const group = "phase9.deletetopiclab.group"

var scopedTables = []string{"cursor", "lease", "binding", "latest_key"}

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	topicName := fmt.Sprintf("phase9.deletetopiclab.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, &topic.Config{Name: topicName, PartitionSize: 1000})
	must(err)

	cd, err := consumer.NewConsumerDatastore[common.Work](ds, nil)
	must(err)
	wp, err := producer.NewMessageProducer[common.Work](tp, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	step("seed a row in every topic-scoped table")

	must(cd.UpsertCursor(ctx, tp.Id, group))
	must(cd.Bind(ctx, tp.Id, group, "orders.*"))

	fn := func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}
	// CompactionKey seeds latest_key; the default (protected) idempotency
	// claim seeds idempotency_key -- one Produce call, two tables.
	_, err = wp.ProduceFunc(ctx, fn, producer.ProduceOptions{RoutingKey: "orders.created", CompactionKey: "seed-key"})
	must(err)

	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, 10, 3, 5*time.Second, false)
	must(err)
	if claim == nil {
		die("expected a claim, got nil")
	}
	// deliberately never Commit -- leaves the lease open

	must(cd.FanOut(ctx, tp.Id, group, 100)) // materializes a 'ready' delivery row, left unclaimed

	// claim it via the lifecycle path and fail it once -- status flips
	// ready->inflight->ready in place (still 1 delivery row) while parking one
	// delivery_log row, without touching cursor/lease (lifecycle path skips both).
	claimedLifecycle, err := cd.ClaimMessagesWithLifecycle(ctx, tp.Id, group, 10)
	must(err)
	if len(claimedLifecycle) != 1 {
		die(fmt.Sprintf("expected 1 lifecycle claim, got %d", len(claimedLifecycle)))
	}
	must(cd.RecordFailure(ctx, 3, &claimedLifecycle[0], errors.New("seed failure"), tp.DisableDeliveryLog))

	for _, table := range scopedTables {
		assertRowCount(ctx, ds, table, tp.Id, 1, "before Destroy")
	}
	assertTableExists(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), true)
	assertDeliveryRowCount(ctx, ds, tp.Id, 1, "before Destroy")
	assertTableExists(ctx, ds, fmt.Sprintf("delivery_log_%d", tp.Id), true)
	assertDeliveryLogRowCount(ctx, ds, tp.Id, 1, "before Destroy")
	assertTableExists(ctx, ds, fmt.Sprintf("idempotency_key_%d", tp.Id), true)
	assertIdempotencyKeyRowCount(ctx, ds, tp.Id, 1, "before Destroy")

	step("Destroy the topic")
	must(topic.Destroy(ctx, ds, topicName))

	for _, table := range scopedTables {
		assertRowCount(ctx, ds, table, tp.Id, 0, "after Destroy")
	}
	assertTableExists(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), false)
	assertTableExists(ctx, ds, fmt.Sprintf("delivery_%d", tp.Id), false)
	assertTableExists(ctx, ds, fmt.Sprintf("delivery_log_%d", tp.Id), false)
	assertTableExists(ctx, ds, fmt.Sprintf("idempotency_key_%d", tp.Id), false)

	fmt.Println("\n✅ DELETE TOPIC CASCADE LAB PASSED")
	fmt.Println("   cursor/lease/binding/latest_key are all cleaned up on Destroy, the")
	fmt.Println("   per-topic delivery/delivery_log/idempotency_key tables are all dropped")
	fmt.Println("   outright, and neither the still-open lease nor the unclaimed delivery row")
	fmt.Println("   survive.")
}

// ---- helpers ----

func assertRowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, table string, topicID int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE topic_id = $1;`, table), topicID).Scan(&count))
	if count != want {
		die(fmt.Sprintf("%s[topic %d] has %d rows %s, want %d", table, topicID, count, when, want))
	}
	fmt.Printf("  ✓ %s has %d row(s) %s\n", table, count, when)
}

// assertDeliveryRowCount counts delivery_<topicID>'s rows directly -- unlike
// scopedTables, this table has no topic_id column to filter by (it's implicit
// in the table name), so it can't go through assertRowCount's generic form.
func assertDeliveryRowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM delivery_%d;`, topicID)).Scan(&count))
	if count != want {
		die(fmt.Sprintf("delivery_%d has %d rows %s, want %d", topicID, count, when, want))
	}
	fmt.Printf("  ✓ delivery_%d has %d row(s) %s\n", topicID, count, when)
}

// assertDeliveryLogRowCount counts delivery_log_<topicID>'s rows directly --
// same no-topic_id-column reason as assertDeliveryRowCount.
func assertDeliveryLogRowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM delivery_log_%d;`, topicID)).Scan(&count))
	if count != want {
		die(fmt.Sprintf("delivery_log_%d has %d rows %s, want %d", topicID, count, when, want))
	}
	fmt.Printf("  ✓ delivery_log_%d has %d row(s) %s\n", topicID, count, when)
}

// assertIdempotencyKeyRowCount counts idempotency_key_<topicID>'s rows
// directly -- same no-topic_id-column reason as assertDeliveryRowCount.
func assertIdempotencyKeyRowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM idempotency_key_%d;`, topicID)).Scan(&count))
	if count != want {
		die(fmt.Sprintf("idempotency_key_%d has %d rows %s, want %d", topicID, count, when, want))
	}
	fmt.Printf("  ✓ idempotency_key_%d has %d row(s) %s\n", topicID, count, when)
}

func assertTableExists(ctx context.Context, ds *coredatastore.PostgresDatastore, table string, want bool) {
	var exists *string
	must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1)::text;`, table).Scan(&exists))
	got := exists != nil
	if got != want {
		die(fmt.Sprintf("%s exists=%v, want %v", table, got, want))
	}
	fmt.Printf("  ✓ %s exists=%v\n", table, got)
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
