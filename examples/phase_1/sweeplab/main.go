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
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	janitordatastore "github.com/agentstax/vulkan/pkg/topic/janitor/controller/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const (
	partitionSize = int64(1000000) // matches migration 001's original message_log_0 width -- no schema swap this lab
	ttl           = 100 * time.Millisecond
	ttlMargin     = 300 * time.Millisecond
	batchSize     = 1000
)

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

	topicName := fmt.Sprintf("phase8a.sweeplab.%d", time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{PartitionSize: partitionSize})
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, vulkan.DestroyOptions{Force: true}))
	}()

	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)
	janitorDatastore, err := janitordatastore.NewJanitorDatastore(ds, nil)
	must(err)

	step("publish 4 'old' messages, then let them age past ttl")
	head0 := head(ctx, ds, tp.Id)
	for range 4 {
		publish(ctx, wpInstance)
	}
	oldLow, oldHigh := head0, head0+4
	time.Sleep(ttl + ttlMargin)

	step("publish 3 'fresh' messages -- well inside ttl")
	freshLow, freshHigh := head(ctx, ds, tp.Id), head(ctx, ds, tp.Id)+3
	for range 3 {
		publish(ctx, wpInstance)
	}
	fmt.Printf("  old ids (%d,%d], fresh ids (%d,%d]\n", oldLow, oldHigh, freshLow, freshHigh)

	step("DropExpiredPartitions -- no-op, the topic's first partition is still active at this volume")
	must(janitorDatastore.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, tp.DeliveryLogMode))
	assertInt("partition 0 survives", partitionCount(ctx, ds, tp.Id), 1)
	assertInt("old rows untouched by drop", countInRange(ctx, ds, tp.Id, oldLow, oldHigh), 4)
	assertInt("fresh rows untouched by drop", countInRange(ctx, ds, tp.Id, freshLow, freshHigh), 3)

	step("SweepExpiredPartitions -- deletes exactly the expired prefix")
	must(janitorDatastore.SweepExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, batchSize, tp.DeliveryLogMode))
	assertInt("old rows swept", countInRange(ctx, ds, tp.Id, oldLow, oldHigh), 0)
	assertInt("fresh rows survive -- not yet past ttl", countInRange(ctx, ds, tp.Id, freshLow, freshHigh), 3)
	assertInt("partition 0 itself survives -- sweep deletes rows, not partitions", partitionCount(ctx, ds, tp.Id), 1)

	fmt.Println("\n✅ SWEEP LAB PASSED")
	fmt.Println("   a partition too low-volume to ever earn a whole-partition drop still sheds its")
	fmt.Println("   expired prefix via the sweep -- drop and sweep cover each other's weak end.")
	return nil
}

// ---- helpers ----

func publish(ctx context.Context, wpInstance *vulkan.ProducerInstance[common.Work]) {
	_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx, _ string) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, vulkan.ProduceOptions{})
	must(err)
}

func head(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT COALESCE(MAX(id), 0) FROM message_log_%d`, topicId))
}

func countInRange(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId, low, high int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d WHERE id > $1 AND id <= $2`, topicId), low, high)
}

func partitionCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'message_log_%d'::regclass;
	`, topicId))
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
