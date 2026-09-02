package main

// DeleteTopic cascade lab: confirms Destroy doesn't just drop message_log and
// the topic row -- the row delete must cascade the topic's consumer groups
// (and their cursor/binding), its maintenance duties and migration history,
// while the FK-less state (leases, compaction_head) is deleted around it and
// the per-topic delivery_<id>/delivery_log_<id>/idempotency_key_<id> tables
// are dropped outright, or that state is permanently orphaned (nothing else
// ever deletes it).
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
	"github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	deliveryconsumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/deliveryconsumer/controller"
	messageconsumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const group = "phase9.deletetopiclab.group"

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

	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("phase9.deletetopiclab.%d", time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{PartitionSize: 1000})
	must(err)

	cd, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	messageConsumers, err := messageconsumergroupcontroller.NewMessageConsumerGroupController(ds, nil)
	must(err)
	deliveryConsumers, err := deliveryconsumergroupcontroller.NewDeliveryConsumerGroupController(ds, nil)
	must(err)
	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)

	step("seed a row in every topic-scoped table")

	groupId := mustGroupID(cd.RegisterGroup(ctx, tp.Id, group, consumergroup.Beginning()))
	_, err = cd.DeclareBindings(ctx, tp.Id, groupId, []string{"orders.*"}, time.Now())
	must(err)

	fn := func(ctx context.Context, tx vulkan.Tx, _ string) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}
	// Compaction seeds compaction_head; the default (protected) idempotency
	// claim seeds idempotency_key -- one Produce call, two tables.
	compaction, err := vulkan.NewCompactionOptions(0)
	must(err)
	_, err = wpInstance.ProduceFunc(ctx, fn, &vulkan.ProduceOptions{RoutingKey: "orders.created", MessageKey: "seed-key", Compaction: compaction})
	must(err)

	claim, err := messageConsumers.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, 10, 3, 5*time.Second, topic.DeliveryLogModeFailures)
	must(err)
	if claim == nil {
		die("expected a claim, got nil")
	}
	// deliberately never Commit -- leaves the lease open

	must(deliveryConsumers.FanOut(ctx, tp.Id, groupId, 1, 100)) // materializes a 'ready' delivery row, left unclaimed

	// claim it via the lifecycle path and fail it once -- status flips
	// ready->inflight->ready in place (still 1 delivery row) while writing one
	// delivery_log row, without touching cursor/lease (lifecycle path skips both).
	claimedLifecycle, err := deliveryConsumers.ClaimMessagesWithLifecycle(ctx, tp.Id, groupId, 10)
	must(err)
	if len(claimedLifecycle) != 1 {
		die(fmt.Sprintf("expected 1 lifecycle claim, got %d", len(claimedLifecycle)))
	}
	must(deliveryConsumers.RecordFailure(ctx, 3, &claimedLifecycle[0], errors.New("seed failure"), tp.DeliveryLogMode))

	for _, table := range []string{"consumer_group_cursor", "claim_lease", "binding_config"} {
		assertGroupRowCount(ctx, ds, fmt.Sprintf("%s_%d", table, tp.Id), groupId, 1, "before Destroy")
	}
	assertCompactionHeadCount(ctx, ds, tp.Id, 1, "before Destroy")
	assertTableExists(ctx, ds, fmt.Sprintf("%s.%s", ds.Schema, topic.MessageLogTable(tp.Id)), true)
	assertDeliveryRowCount(ctx, ds, tp.Id, 1, "before Destroy")
	assertTableExists(ctx, ds, fmt.Sprintf("%s.%s", ds.Schema, topic.DeliveryLogTable(tp.Id)), true)
	assertDeliveryLogRowCount(ctx, ds, tp.Id, 1, "before Destroy")
	assertTableExists(ctx, ds, fmt.Sprintf("%s.%s", ds.Schema, topic.IdempotencyKeyTable(tp.Id)), true)
	assertIdempotencyKeyRowCount(ctx, ds, tp.Id, 1, "before Destroy")

	step("Destroy the topic")
	must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))

	assertGroupGone(ctx, ds, groupId)
	for _, table := range []string{
		"message_log", "exception_queue", "delivery_log", "idempotency_key",
		"consumer_group_cursor", "claim_lease", "message_key_lease", "compaction_head", "binding_config", "binding_config_log",
	} {
		assertTableExists(ctx, ds, fmt.Sprintf("%s_%d", table, tp.Id), false)
	}

	fmt.Println("\n✅ DELETE TOPIC CASCADE LAB PASSED")
	fmt.Println("   the topic's groups die with it via the topic_id FK cascade, and all ten")
	fmt.Println("   per-topic tables are dropped outright -- neither the still-open lease nor")
	fmt.Println("   the unclaimed delivery row survive.")
	return nil
}

// ---- helpers ----

func assertGroupRowCount(ctx context.Context, ds *iDatastore.PostgresDatastore, table string, groupId int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE consumer_group_id = $1;`, table), groupId).Scan(&count))
	if count != want {
		die(fmt.Sprintf("%s[group %d] has %d rows %s, want %d", table, groupId, count, when, want))
	}
	fmt.Printf("  ✓ %s has %d row(s) %s\n", table, count, when)
}

func assertCompactionHeadCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s;`, ds.Schema, topic.CompactionHeadTable(topicId))).Scan(&count))
	if count != want {
		die(fmt.Sprintf("compaction_head[topic %d] has %d rows %s, want %d", topicId, count, when, want))
	}
	fmt.Printf("  ✓ compaction_head has %d row(s) %s\n", count, when)
}

// the topic's groups are destroyed WITH it, via the topic_id FK cascade.
func assertGroupGone(ctx context.Context, ds *iDatastore.PostgresDatastore, groupId int64) {
	var rows int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM consumer_group_config WHERE id = $1;`, groupId).Scan(&rows))
	if rows != 0 {
		die(fmt.Sprintf("consumer_group %d survived its topic's Destroy", groupId))
	}
	fmt.Printf("  ✓ the topic's group destroyed with it\n")
}

// assertDeliveryRowCount counts delivery_<topicId>'s rows directly -- unlike
// scopedTables, this table has no topic_id column to filter by (it's implicit
// in the table name), so it can't go through assertRowCount's generic form.
func assertDeliveryRowCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s;`, ds.Schema, topic.ExceptionQueueTable(topicId))).Scan(&count))
	if count != want {
		die(fmt.Sprintf("%s.%s has %d rows %s, want %d", ds.Schema, topic.ExceptionQueueTable(topicId), count, when, want))
	}
	fmt.Printf("  ✓ exception_queue_%d has %d row(s) %s\n", topicId, count, when)
}

// assertDeliveryLogRowCount counts delivery_log_<topicId>'s rows directly --
// same no-topic_id-column reason as assertDeliveryRowCount.
func assertDeliveryLogRowCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s;`, ds.Schema, topic.DeliveryLogTable(topicId))).Scan(&count))
	if count != want {
		die(fmt.Sprintf("%s.%s has %d rows %s, want %d", ds.Schema, topic.DeliveryLogTable(topicId), count, when, want))
	}
	fmt.Printf("  ✓ delivery_log_%d has %d row(s) %s\n", topicId, count, when)
}

// assertIdempotencyKeyRowCount counts idempotency_key_<topicId>'s rows
// directly -- same no-topic_id-column reason as assertDeliveryRowCount.
func assertIdempotencyKeyRowCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, want int, when string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s;`, ds.Schema, topic.IdempotencyKeyTable(topicId))).Scan(&count))
	if count != want {
		die(fmt.Sprintf("%s.%s has %d rows %s, want %d", ds.Schema, topic.IdempotencyKeyTable(topicId), count, when, want))
	}
	fmt.Printf("  ✓ idempotency_key_%d has %d row(s) %s\n", topicId, count, when)
}

func assertTableExists(ctx context.Context, ds *iDatastore.PostgresDatastore, table string, want bool) {
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
	panic(labFailure{message: msg})
}

func mustGroupID(g *consumergroup.GroupData, err error) int64 { must(err); return g.Id }
