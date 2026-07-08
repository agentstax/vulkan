package main

// Phase 8a lab (a): id-range partitioning prunes claim reads to 1-2 partitions.
//
// message_log_0 ships from migration 001 at a 1,000,000-row width -- too wide
// to demonstrate pruning without publishing a million rows first. This lab
// swaps in a lab-scale width for its own duration (drop+recreate message_log,
// same shape migration 001 leaves it in, just a tiny first partition), then
// restores the migration's original shape before exiting, deferred so it
// runs even on failure. No FK ties message_log to cursors/deliveries/leases,
// so the swap can't strand rows in those tables -- it only discards
// message_log's own rows, which is why this lab owns the whole table rather
// than just its own group's slice of it.
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

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	"github.com/agentstax/vulkan/pkg/producer"
	prodstore "github.com/agentstax/vulkan/pkg/producer/datastore"
	"github.com/jackc/pgx/v5"
)

const partitionSize = int64(5)

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

	step("swap message_log to a lab-scale partition width (5 rows) -- restored on exit")
	shrinkMessageLog(ctx, cd)
	defer restoreMessageLog(ctx, cd)

	step("publish 14 messages, EnsureNextPartition after each (mirrors the real janitor tick)")
	for range 14 {
		publish(ctx, wp)
		must(cd.EnsureNextPartition(ctx, partitionSize, 1))
	}
	partitionCount := countPartitions(ctx, cd)
	fmt.Printf("  %d partitions exist (0-2 hold ids 1-14, 3 is create-ahead headroom)\n", partitionCount)
	assertInt("4 partitions exist at width 5", partitionCount, 4)

	step("EXPLAIN (0,3] -- entirely inside message_log_0")
	explainReadMessages(ctx, cd, 0, 3, 1)

	step("EXPLAIN (3,8] -- straddles message_log_0 / message_log_1")
	explainReadMessages(ctx, cd, 3, 8, 2)

	step("EXPLAIN (8,9] -- entirely inside message_log_1")
	explainReadMessages(ctx, cd, 8, 9, 1)

	fmt.Println("\n✅ PARTITION PRUNING LAB PASSED")
	fmt.Println("   a claim's id range only ever touches the partition(s) it overlaps --")
	fmt.Println("   pruning payoff observed via EXPLAIN, not assumed.")
}

// ---- schema swap ----

// shrinkMessageLog replaces message_log with migration 001's shape, except
// message_log_0 is lab-width instead of 1,000,000 rows -- wide enough there
// to ever see more than one partition in a lab run.
func shrinkMessageLog(ctx context.Context, cd *consumer.PostgresDatastore[common.Work]) {
	recreateMessageLog(ctx, cd, partitionSize)
}

// restoreMessageLog puts message_log back exactly as migration 001 leaves it.
func restoreMessageLog(ctx context.Context, cd *consumer.PostgresDatastore[common.Work]) {
	recreateMessageLog(ctx, cd, 1000000)
	fmt.Println("  message_log restored to migration 001 shape")
}

func recreateMessageLog(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], firstPartitionWidth int64) {
	must(exec(ctx, cd, `DROP TABLE IF EXISTS message_log CASCADE;`))
	must(exec(ctx, cd, `
		CREATE TABLE message_log (
			id BIGSERIAL PRIMARY KEY,
			routing_key TEXT,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		) PARTITION BY RANGE (id);
	`))
	must(exec(ctx, cd, fmt.Sprintf(`
		CREATE TABLE message_log_0
			PARTITION OF message_log
			FOR VALUES FROM (0) TO (%d);
	`, firstPartitionWidth)))
}

// ---- helpers ----

func publish(ctx context.Context, wp *producer.WorkProducer[common.Work]) {
	_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, "")
	must(err)
}

func countPartitions(ctx context.Context, cd *consumer.PostgresDatastore[common.Work]) int64 {
	return scalar(ctx, cd, `
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'message_log'::regclass;
	`)
}

// explainReadMessages EXPLAINs the exact query readMessages runs on a claim
// (WHERE m.id > low AND m.id <= high) and counts distinct message_log_N
// partitions named anywhere in the plan -- pruned partitions never appear.
func explainReadMessages(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], low, high int64, want int) {
	sql := `
		EXPLAIN SELECT m.id, m.payload, m.created_at FROM message_log m
		WHERE m.id > $1
			AND m.id <= $2
			AND (
				NOT EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group = $3)
				OR EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group = $3 AND m.routing_key ~ b.pattern)
			)
		ORDER BY m.id;
	`
	rows, err := cd.Pool.Query(ctx, sql, low, high, "phase8a.partition.lab")
	must(err)
	defer rows.Close()

	partitionRe := regexp.MustCompile(`message_log_\d+`)
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

func exec(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], sql string) error {
	_, err := cd.Pool.Exec(ctx, sql)
	return err
}

func scalar(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], q string, args ...any) int64 {
	var v int64
	must(cd.Pool.QueryRow(ctx, q, args...).Scan(&v))
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
