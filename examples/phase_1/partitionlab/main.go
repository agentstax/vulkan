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
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"regexp"
	"sort"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const partitionSize = int64(5)

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

	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()

	topicName := fmt.Sprintf("phase8a.partitionlab.%d", time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{PartitionSize: partitionSize})
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)

	step("publish 14 messages -- each partition's 80% trigger creates the next ahead")
	for i := int64(1); i <= 14; i++ {
		publish(ctx, wpInstance)
		// ids 4/9/14 are the 80% trigger points at width 5. Creation is a
		// detached goroutine racing the next publish's insert (which would
		// heal-and-burn an id if it won) -- waiting here keeps the lab's id
		// layout deterministic: 14 rows land contiguously as ids 1-14.
		if i == 4 || i == 9 || i == 14 {
			waitForPartition(ctx, ds, tp.Id, i/partitionSize+1)
		}
	}
	partitionCount := countPartitions(ctx, ds, tp.Id)
	fmt.Printf("  %d partitions exist (0-3, 1-3 each created ahead at the prior partition's trigger)\n", partitionCount)
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
	return nil
}

// ---- helpers ----

func publish(ctx context.Context, wpInstance *vulkan.ProducerInstance[common.Work]) {
	_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx, _ string) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, nil)
	must(err)
}

func waitForPartition(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, n int64) {
	table := fmt.Sprintf("%s.%s", ds.Schema, topic.MessageLogPartitionTable(topicId, n))
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		var exists bool
		must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL;`, table).Scan(&exists))
		if exists {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	die(fmt.Sprintf("%s was not created ahead within 10s", table))
}

func countPartitions(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = '%s.%s'::regclass;
	`, ds.Schema, topic.MessageLogTable(topicId)))
}

// explainReadMessages EXPLAINs the exact query readMessages runs on a claim
// (WHERE m.id > low AND m.id <= high) and counts distinct message_log_<id>_N
// partitions named anywhere in the plan -- pruned partitions never appear.
func explainReadMessages(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId, low, high int64, want int) {
	logName := topic.MessageLogTable(topicId)
	logTable := fmt.Sprintf("%s.%s", ds.Schema, logName)
	bindingTable := fmt.Sprintf("%s.%s", ds.Schema, topic.BindingConfigTable(topicId))
	sql := fmt.Sprintf(`
		EXPLAIN SELECT m.id, m.payload, m.created_at FROM %s m
		WHERE m.id > $1
			AND m.id <= $2
			AND (
				NOT EXISTS (SELECT 1 FROM %s b WHERE b.consumer_group_id = $3)
				OR EXISTS (SELECT 1 FROM %s b WHERE b.consumer_group_id = $3 AND m.routing_key ~ b.pattern_regex)
			)
		ORDER BY m.id;
	`, logTable, bindingTable, bindingTable)
	rows, err := ds.Pool.Query(ctx, sql, low, high, 0) // no binding rows exist for group id 0 -- the NOT EXISTS arm is what the plan exercises
	must(err)
	defer rows.Close()

	partitionRe := regexp.MustCompile(regexp.QuoteMeta(logName) + `_\d+`)
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

func scalar(ctx context.Context, ds *iDatastore.PostgresDatastore, q string, args ...any) int64 {
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
	panic(labFailure{message: msg})
}
func assertInt(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
