package main

// Phase 8a lab (c): the low-volume tail -- a partition that never fills wide
// enough to earn a whole-partition drop still needs its expired rows to
// leave. No schema swap here, unlike partitionlab/dropfloorlab: this lab
// stays entirely inside message_log_0 at its real migration-shipped width,
// because staying under that width (never rolling to a second partition) is
// exactly the condition the sweep exists to cover.
//
// Uses AllowDropPastCommitted=true throughout so the outcome depends only on
// TTL/partition-activity, not on whatever committed state other labs' cursor
// rows happen to be left at -- the drop floor is global across every group
// sharing message_log (a known limitation, see cursorFloor's TODO), so a
// lab that shares the dev DB with others can't assume a clean floor.
//
// Confirms: DropExpiredPartitions is a no-op here (message_log_0 is still
// the active partition, nowhere near partitionSize, so the whole-partition
// path never engages at this volume) while SweepExpiredPartitions deletes
// exactly the expired prefix and leaves the fresher rows and the partition
// itself untouched.

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
	partitionSize = int64(1000000) // matches migration 001's message_log_0 width -- no schema swap this lab
	ttl           = 100 * time.Millisecond
	ttlMargin     = 300 * time.Millisecond
	batchSize     = 1000
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

	step("publish 4 'old' messages, then let them age past ttl")
	head0 := head(ctx, cd)
	for range 4 {
		publish(ctx, wp)
	}
	oldLow, oldHigh := head0, head0+4
	time.Sleep(ttl + ttlMargin)

	step("publish 3 'fresh' messages -- well inside ttl")
	freshLow, freshHigh := head(ctx, cd), head(ctx, cd)+3
	for range 3 {
		publish(ctx, wp)
	}
	fmt.Printf("  old ids (%d,%d], fresh ids (%d,%d]\n", oldLow, oldHigh, freshLow, freshHigh)

	step("DropExpiredPartitions -- no-op, message_log_0 is still active at this volume")
	must(cd.DropExpiredPartitions(ctx, partitionSize, ttl, true))
	assertInt("message_log_0 survives", partitionCount(ctx, cd), 1)
	assertInt("old rows untouched by drop", countInRange(ctx, cd, oldLow, oldHigh), 4)
	assertInt("fresh rows untouched by drop", countInRange(ctx, cd, freshLow, freshHigh), 3)

	step("SweepExpiredPartitions -- deletes exactly the expired prefix")
	must(cd.SweepExpiredPartitions(ctx, partitionSize, ttl, true, batchSize))
	assertInt("old rows swept", countInRange(ctx, cd, oldLow, oldHigh), 0)
	assertInt("fresh rows survive -- not yet past ttl", countInRange(ctx, cd, freshLow, freshHigh), 3)
	assertInt("message_log_0 itself survives -- sweep deletes rows, not partitions", partitionCount(ctx, cd), 1)

	fmt.Println("\n✅ SWEEP LAB PASSED")
	fmt.Println("   a partition too low-volume to ever earn a whole-partition drop still sheds its")
	fmt.Println("   expired prefix via the sweep -- drop and sweep cover each other's weak end.")
}

// ---- helpers ----

func publish(ctx context.Context, wp *producer.WorkProducer[common.Work]) {
	_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, "")
	must(err)
}

func head(ctx context.Context, cd *consumer.PostgresDatastore[common.Work]) int64 {
	return scalar(ctx, cd, `SELECT COALESCE(MAX(id), 0) FROM message_log`)
}

func countInRange(ctx context.Context, cd *consumer.PostgresDatastore[common.Work], low, high int64) int64 {
	return scalar(ctx, cd, `SELECT count(*) FROM message_log WHERE id > $1 AND id <= $2`, low, high)
}

func partitionCount(ctx context.Context, cd *consumer.PostgresDatastore[common.Work]) int64 {
	return scalar(ctx, cd, `
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'message_log'::regclass;
	`)
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
