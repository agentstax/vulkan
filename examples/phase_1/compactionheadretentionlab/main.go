package main

// log compaction + retention lab: does 8a's retention correctly garbage
// collect compaction_head when it reaps a compacted key's last surviving row?
//
// Two scenarios, one per janitor path a topic's PartitionSize routes it
// through:
//   - dropPartition: a small PartitionSize rolls a dormant key's sole
//     partition out of active use and past ttl; DropExpiredPartitions
//     removes the whole partition and must take compaction_head's now-dangling
//     pointer with it.
//   - sweepBatch: a large PartitionSize keeps everything in partition 0
//     forever; SweepExpiredPartitions reaps the individually-expired row
//     from the front and must do the identical compaction_head cleanup.
//
// A key touched again inside the ttl window proves the opposite case in
// each scenario too: retention doing nothing to a key that's still alive --
// this is intentional expiration, not compaction-awareness bolted onto
// retention (decision record [0269] in docs/decisions/).

import (
	"context"
	"fmt"
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	janitordatastore "github.com/agentstax/vulkan/pkg/topic/janitor/controller/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const (
	ttl       = 100 * time.Millisecond
	ttlMargin = 300 * time.Millisecond
	batchSize = 1000
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

	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	ds, err := iDatastore.NewPostgresDatastore(ctx, pool, nil)
	must(err)

	dropPartitionScenario(ctx, ds)
	sweepBatchScenario(ctx, ds)

	fmt.Println("\n✅ LATEST KEYS RETENTION LAB PASSED")
	fmt.Println("   a dormant key's last row aging out takes its compaction_head pointer with it,")
	fmt.Println("   exactly like Kafka's own cleanup.policy=compact,delete -- a key touched")
	fmt.Println("   inside the ttl window survives every pass untouched, either path.")
	return nil
}

func dropPartitionScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("dropPartition: a whole-partition rollover reaps a dormant key's last row")

	const partitionSize = int64(4)
	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("phase8c.compactionheadretentionlab.drop.%d", time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{PartitionSize: partitionSize})
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)
	janitorDatastore, err := janitordatastore.NewJanitorDatastore(ds, nil)
	must(err)

	// fill partition 0 with a dormant key + filler, then age past ttl
	publish(ctx, wpInstance, "dormant-key")
	publish(ctx, wpInstance, "")
	publish(ctx, wpInstance, "")
	publish(ctx, wpInstance, "")
	time.Sleep(ttl + ttlMargin)

	// roll into partition 1 so partition 0 is no longer active
	publish(ctx, wpInstance, "alive-key")
	publish(ctx, wpInstance, "")
	publish(ctx, wpInstance, "")
	publish(ctx, wpInstance, "")

	assertLatestExists(ctx, ds, tp.Id, "dormant-key", true)
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)

	must(janitorDatastore.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, tp.DeliveryLogMode))

	assertLatestExists(ctx, ds, tp.Id, "dormant-key", false)
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)
}

func sweepBatchScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("sweepBatch: a low-volume tail reaps a dormant key's last row individually")

	const partitionSize = int64(1000000) // matches migration 001's original width -- never rolls
	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("phase8c.compactionheadretentionlab.sweep.%d", time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{PartitionSize: partitionSize})
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)
	janitorDatastore, err := janitordatastore.NewJanitorDatastore(ds, nil)
	must(err)

	publish(ctx, wpInstance, "dormant-key")
	time.Sleep(ttl + ttlMargin)
	publish(ctx, wpInstance, "alive-key") // well inside ttl

	assertLatestExists(ctx, ds, tp.Id, "dormant-key", true)
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)

	must(janitorDatastore.SweepExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, batchSize, tp.DeliveryLogMode))

	assertLatestExists(ctx, ds, tp.Id, "dormant-key", false)
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)

	// sweep repeatedly -- a key kept alive inside ttl survives every pass, not just the first
	for range 3 {
		publish(ctx, wpInstance, "alive-key")
		time.Sleep(ttl / 4)
		must(janitorDatastore.SweepExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, batchSize, tp.DeliveryLogMode))
	}
	assertLatestExists(ctx, ds, tp.Id, "alive-key", true)
}

// ---- helpers ----

func publish(ctx context.Context, wpInstance *vulkan.ProducerInstance[common.Work], key string) {
	opts := &vulkan.ProduceOptions{}
	if key != "" {
		compaction, err := vulkan.NewCompactionOptions(0)
		must(err)
		opts.MessageKey = key
		opts.Compaction = compaction
	}
	_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx, _ string) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, opts)
	must(err)
}

func assertLatestExists(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, key string, want bool) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE compaction_key=$1;`, ds.Schema, topic.CompactionHeadTable(topicId)), key).Scan(&count))
	got := count > 0
	if got != want {
		die(fmt.Sprintf("compaction_head[%s] exists=%v, want %v", key, got, want))
	}
	fmt.Printf("  ✓ compaction_head[%s] exists=%v\n", key, got)
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
