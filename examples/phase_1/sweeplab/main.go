package main

// Phase 8a lab (c): the low-volume tail -- a partition that never fills wide
// enough to earn a whole-partition drop still needs its expired rows to leave.
//
// Registers its own topic at the real migration-shipped partition width
// (1,000,000), destroyed on exit -- staying under that width (never rolling to
// a second partition) is exactly the condition the sweep exists to cover, so no
// schema swap is needed, unlike partitionlab/dropfloorlab. A dedicated topic
// also means this lab's own cursorFloor is isolated from every other lab and
// group sharing the dev DB, so unlike the pre-8b version it no longer needs to
// force AllowDropPastCommitted=true just to dodge a floor some unrelated
// group's leftover state might be pinning.
//
// Confirms: DropExpiredPartitions is a no-op here (the topic's first partition
// is still active, nowhere near partitionSize, so the whole-partition path
// never engages at this volume) while SweepExpiredPartitions deletes exactly
// the expired prefix and leaves the fresher rows and the partition itself
// untouched.

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
	partitionSize = int64(1000000) // matches migration 001's original message_log_0 width -- no schema swap this lab
	ttl           = 100 * time.Millisecond
	ttlMargin     = 300 * time.Millisecond
	batchSize     = 1000
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("phase8a.sweeplab.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: partitionSize})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)
	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)

	step("publish 4 'old' messages, then let them age past ttl")
	head0 := head(ctx, ds, tp.Id)
	for range 4 {
		publish(ctx, wp)
	}
	oldLow, oldHigh := head0, head0+4
	time.Sleep(ttl + ttlMargin)

	step("publish 3 'fresh' messages -- well inside ttl")
	freshLow, freshHigh := head(ctx, ds, tp.Id), head(ctx, ds, tp.Id)+3
	for range 3 {
		publish(ctx, wp)
	}
	fmt.Printf("  old ids (%d,%d], fresh ids (%d,%d]\n", oldLow, oldHigh, freshLow, freshHigh)

	step("DropExpiredPartitions -- no-op, the topic's first partition is still active at this volume")
	must(cd.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true))
	assertInt("partition 0 survives", partitionCount(ctx, ds, tp.Id), 1)
	assertInt("old rows untouched by drop", countInRange(ctx, ds, tp.Id, oldLow, oldHigh), 4)
	assertInt("fresh rows untouched by drop", countInRange(ctx, ds, tp.Id, freshLow, freshHigh), 3)

	step("SweepExpiredPartitions -- deletes exactly the expired prefix")
	must(cd.SweepExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, batchSize))
	assertInt("old rows swept", countInRange(ctx, ds, tp.Id, oldLow, oldHigh), 0)
	assertInt("fresh rows survive -- not yet past ttl", countInRange(ctx, ds, tp.Id, freshLow, freshHigh), 3)
	assertInt("partition 0 itself survives -- sweep deletes rows, not partitions", partitionCount(ctx, ds, tp.Id), 1)

	fmt.Println("\n✅ SWEEP LAB PASSED")
	fmt.Println("   a partition too low-volume to ever earn a whole-partition drop still sheds its")
	fmt.Println("   expired prefix via the sweep -- drop and sweep cover each other's weak end.")
}

// ---- helpers ----

func publish(ctx context.Context, wp *producer.WorkProducer[common.Work]) {
	_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, producer.ProduceOptions{})
	must(err)
}

func head(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT COALESCE(MAX(id), 0) FROM message_log_%d`, topicID))
}

func countInRange(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, low, high int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d WHERE id > $1 AND id <= $2`, topicID), low, high)
}

func partitionCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'message_log_%d'::regclass;
	`, topicID))
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
