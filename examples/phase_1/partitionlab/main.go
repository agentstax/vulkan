package main

// Phase 8a lab (a): id-range partitioning prunes claim reads to 1-2 partitions.
//
// Registers its own topic with a lab-scale PartitionSize (5 rows), destroyed on
// exit -- under 8b, partition width is a per-topic Register() param, so this lab
// no longer needs the pre-8b schema-swap hack (DROP+recreate the shared
// message_log table, restore its 1,000,000-row shape on exit, permanently
// discarding whatever rows were in it). A dedicated topic gets its own
// message_log_<id> at exactly the width this lab wants, and Destroy cleans it
// up without touching anything else.
//
// Confirms: EXPLAINing the real claim-path read query (readMessages' WHERE
// m.id > low AND m.id <= high) prunes to exactly the partition(s) a range
// overlaps -- 1 when it stays inside one partition, 2 when it straddles a
// boundary -- never scanning a partition the range doesn't reach.

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	prodstore "github.com/agentstax/vulkan/pkg/producer/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
)

const partitionSize = int64(5)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("phase8a.partitionlab.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: partitionSize})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds)
	pd := prodstore.NewProducerDatastore[common.Work](ds)
	wp := producer.NewWorkProducer(tp, pd)

	step("publish 14 messages, EnsureNextPartition after each (mirrors the real janitor tick)")
	for range 14 {
		publish(ctx, wp)
		must(cd.EnsureNextPartition(ctx, tp.Id, partitionSize, 1))
	}
	partitionCount := countPartitions(ctx, ds, tp.Id)
	fmt.Printf("  %d partitions exist (0-2 hold ids 1-14, 3 is create-ahead headroom)\n", partitionCount)
	assertInt("4 partitions exist at width 5", partitionCount, 4)

	step("EXPLAIN (0,3] -- entirely inside message_log_<id>_0")
	explainReadMessages(ctx, ds, tp.Id, 0, 3, 1)

	step("EXPLAIN (3,8] -- straddles message_log_<id>_0 / _1")
	explainReadMessages(ctx, ds, tp.Id, 3, 8, 2)

	step("EXPLAIN (8,9] -- entirely inside message_log_<id>_1")
	explainReadMessages(ctx, ds, tp.Id, 8, 9, 1)

	fmt.Println("\n✅ PARTITION PRUNING LAB PASSED")
	fmt.Println("   a claim's id range only ever touches the partition(s) it overlaps --")
	fmt.Println("   pruning payoff observed via EXPLAIN, not assumed.")
}

// ---- helpers ----

func publish(ctx context.Context, wp *producer.WorkProducer[common.Work]) {
	_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, "")
	must(err)
}

func countPartitions(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'message_log_%d'::regclass;
	`, topicID))
}

// explainReadMessages EXPLAINs the exact query readMessages runs on a claim
// (WHERE m.id > low AND m.id <= high) and counts distinct message_log_<id>_N
// partitions named anywhere in the plan -- pruned partitions never appear.
func explainReadMessages(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, low, high int64, want int) {
	logTable := fmt.Sprintf("message_log_%d", topicID)
	sql := fmt.Sprintf(`
		EXPLAIN SELECT m.id, m.payload, m.created_at FROM %s m
		WHERE m.id > $1
			AND m.id <= $2
			AND (
				NOT EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group = $3 AND b.topic_id = $4)
				OR EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group = $3 AND b.topic_id = $4 AND m.routing_key ~ b.pattern)
			)
		ORDER BY m.id;
	`, logTable)
	rows, err := ds.Pool.Query(ctx, sql, low, high, "phase8a.partition.lab", topicID)
	must(err)
	defer rows.Close()

	partitionRe := regexp.MustCompile(regexp.QuoteMeta(logTable) + `_\d+`)
	touched := map[string]bool{}
	for rows.Next() {
		var line string
		must(rows.Scan(&line))
		for _, m := range partitionRe.FindAllString(line, -1) {
			touched[m] = true
		}
	}
	must(rows.Err())

	names := make([]string, 0, len(touched))
	for n := range touched {
		names = append(names, n)
	}
	sort.Strings(names)

	fmt.Printf("  (%d,%d] plan touches: %v\n", low, high, names)
	assertInt(fmt.Sprintf("(%d,%d] touches exactly %d partition(s)", low, high, want), int64(len(names)), int64(want))
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
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
