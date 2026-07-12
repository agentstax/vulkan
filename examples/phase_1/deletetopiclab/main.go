package main

// DeleteTopic cascade lab: confirms Destroy doesn't just drop message_log and
// the topics row -- it also has to clean up every other table scoped by
// topic_id (cursors, leases, deliveries, bindings, latest_keys,
// idempotency_keys), or those rows are permanently orphaned (nothing else
// ever deletes them by topic_id alone).
//
// Seeds one row in each of the six tables via the real datastore methods,
// deliberately leaving a lease OPEN and a deliveries row unclaimed -- the
// messiest state a topic could be destroyed in mid-flight, not a
// conveniently-already-resolved one.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	prodstore "github.com/agentstax/vulkan/pkg/producer/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const group = "phase9.deletetopiclab.group"

var scopedTables = []string{"cursors", "leases", "deliveries", "bindings", "latest_keys", "idempotency_keys"}

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("phase9.deletetopiclab.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: 1000})
	must(err)

	cd := consumer.NewConsumerDatastore[common.Work](ds)
	pd := prodstore.NewProducerDatastore[common.Work](ds)
	wp := producer.NewWorkProducer(tp, pd)

	step("seed a row in every topic-scoped table")

	must(cd.UpsertCursor(ctx, tp.Id, group))
	must(cd.Bind(ctx, tp.Id, group, "orders.*"))

	fn := func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}
	// CompactionKey seeds latest_keys; the default (protected) idempotency
	// claim seeds idempotency_keys -- one Produce call, two tables.
	_, err = wp.Produce(ctx, fn, producer.ProduceOptions{RoutingKey: "orders.created", CompactionKey: "seed-key"})
	must(err)

	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, 10, 3, 5*time.Second)
	must(err)
	if claim == nil {
		die("expected a claim, got nil")
	}
	// deliberately never Commit -- leaves the lease open

	must(cd.FanOut(ctx, tp.Id, group)) // materializes a 'ready' deliveries row, left unclaimed

	for _, table := range scopedTables {
		assertRowCount(ctx, ds, table, tp.Id, 1, "before Destroy")
	}
	assertLogTableExists(ctx, ds, tp.Id, true)

	step("Destroy the topic")
	must(topic.Destroy(ctx, ds, topicName))

	for _, table := range scopedTables {
		assertRowCount(ctx, ds, table, tp.Id, 0, "after Destroy")
	}
	assertLogTableExists(ctx, ds, tp.Id, false)

	fmt.Println("\n✅ DELETE TOPIC CASCADE LAB PASSED")
	fmt.Println("   cursors/leases/deliveries/bindings/latest_keys/idempotency_keys are all")
	fmt.Println("   cleaned up on Destroy, including a still-open lease and an unclaimed")
	fmt.Println("   deliveries row -- not just the conveniently-already-resolved case.")
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

func assertLogTableExists(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, want bool) {
	var exists *string
	must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1)::text;`, fmt.Sprintf("message_log_%d", topicID)).Scan(&exists))
	got := exists != nil
	if got != want {
		die(fmt.Sprintf("message_log_%d exists=%v, want %v", topicID, got, want))
	}
	fmt.Printf("  ✓ message_log_%d exists=%v\n", topicID, got)
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
