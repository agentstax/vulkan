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
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	janitordatastore "github.com/agentstax/vulkan/pkg/worker/janitor/datastore"
	"github.com/google/uuid"
)

const (
	ttl       = 100 * time.Millisecond
	ttlMargin = 300 * time.Millisecond
	batchSize = 1000
)

func main() {
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, &iDatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	dropPartitionScenario(ctx, ds)
	sweepBatchScenario(ctx, ds)

	fmt.Println("\n✅ LATEST KEYS RETENTION LAB PASSED")
	fmt.Println("   a dormant key's last row aging out takes its compaction_head pointer with it,")
	fmt.Println("   exactly like Kafka's own cleanup.policy=compact,delete -- a key touched")
	fmt.Println("   inside the ttl window survives every pass untouched, either path.")
}

func dropPartitionScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("dropPartition: a whole-partition rollover reaps a dormant key's last row")

	const partitionSize = int64(4)
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("phase8c.compactionheadretentionlab.drop.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{PartitionSize: partitionSize})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
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
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("phase8c.compactionheadretentionlab.sweep.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{PartitionSize: partitionSize})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
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

func publish(ctx context.Context, wpInstance *producer.ProducerInstance[common.Work], key string) {
	_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
		return common.NewWork(30, "admin@example.com")
	}, producer.ProduceOptions{CompactionKey: key})
	must(err)
}

func assertLatestExists(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, key string, want bool) {
	var count int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM compaction_head WHERE topic_id=$1 AND compaction_key=$2;`, topicId, key).Scan(&count))
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
	fmt.Printf("\n❌ LAB FAILED: %s\n", msg)
	os.Exit(1)
}
