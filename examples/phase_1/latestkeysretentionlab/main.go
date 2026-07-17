package main

// log compaction + retention lab: does 8a's retention correctly garbage
// collect latest_key when it reaps a compacted key's last surviving row?
//
// Two scenarios, one per janitor path a topic's PartitionSize routes it
// through:
//   - dropPartition: a small PartitionSize rolls a dormant key's sole
//     partition out of active use and past ttl; DropExpiredPartitions
//     removes the whole partition and must take latest_key's now-dangling
//     pointer with it.
//   - sweepBatch: a large PartitionSize keeps everything in partition 0
//     forever; SweepExpiredPartitions reaps the individually-expired row
//     from the front and must do the identical latest_key cleanup.
//
// A key touched again inside the ttl window proves the opposite case in
// each scenario too: retention doing nothing to a key that's still alive --
// this is intentional expiration, not compaction-awareness bolted onto
// retention (see LEARNING_PLAN.md's 8c "Decided" note).

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
)

const (
	ttl       = 100 * time.Millisecond
	ttlMargin = 300 * time.Millisecond
	batchSize = 1000
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	dropPartitionScenario(ctx, ds)
	sweepBatchScenario(ctx, ds)

	fmt.Println("\n✅ LATEST KEYS RETENTION LAB PASSED")
	fmt.Println("   a dormant key's last row aging out takes its latest_key pointer with it,")
	fmt.Println("   exactly like Kafka's own cleanup.policy=compact,delete -- a key touched")
	fmt.Println("   inside the ttl window survives every pass untouched, either path.")
}

func dropPartitionScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("dropPartition: a whole-partition rollover reaps a dormant key's last row")

	const partitionSize = int64(4)
	topicName := fmt.Sprintf("phase8c.latestkeysretentionlab.drop.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: partitionSize})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)
	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)

	// fill partition 0 with a dormant key + filler, then age past ttl
	publish(ctx, wp, "dormant-key")
	publish(ctx, wp, "")
	publish(ctx, wp, "")
	publish(ctx, wp, "")
	time.Sleep(ttl + ttlMargin)

	// roll into partition 1 so partition 0 is no longer active
	publish(ctx, wp, "alive-key")
	publish(ctx, wp, "")
	publish(ctx, wp, "")
	publish(ctx, wp, "")

	assertLatestExists(ctx, ds, tp.Id, "dormant-key", true)
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)

	must(cd.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, tp.DisableDeliveryLog))

	assertLatestExists(ctx, ds, tp.Id, "dormant-key", false)
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)
}

func sweepBatchScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("sweepBatch: a low-volume tail reaps a dormant key's last row individually")

	const partitionSize = int64(1000000) // matches migration 001's original width -- never rolls
	topicName := fmt.Sprintf("phase8c.latestkeysretentionlab.sweep.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: partitionSize})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)
	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)

	publish(ctx, wp, "dormant-key")
	time.Sleep(ttl + ttlMargin)
	publish(ctx, wp, "alive-key") // well inside ttl

	assertLatestExists(ctx, ds, tp.Id, "dormant-key", true)
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)

	must(cd.SweepExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, batchSize, tp.DisableDeliveryLog))

	assertLatestExists(ctx, ds, tp.Id, "dormant-key", false)
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)

	// sweep repeatedly -- a key kept alive inside ttl survives every pass, not just the first
	for range 3 {
		publish(ctx, wp, "alive-key")
		time.Sleep(ttl / 4)
		must(cd.SweepExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, batchSize, tp.DisableDeliveryLog))
	}
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)
}

// ---- helpers ----

func publish(ctx context.Context, wp *producer.WorkProducer[common.Work], key string) {
	_, err := wp.Produce(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, producer.ProduceOptions{CompactionKey: key})
	must(err)
}

func assertLatestExists(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, key string, want bool) {
	var count int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM latest_key WHERE topic_id=$1 AND compaction_key=$2;`, topicID, key).Scan(&count))
	got := count > 0
	if got != want {
		die(fmt.Sprintf("latest_key[%s] exists=%v, want %v", key, got, want))
	}
	fmt.Printf("  ✓ latest_key[%s] exists=%v\n", key, got)
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
