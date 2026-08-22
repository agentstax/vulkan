// Command workerclaimlab proves worker-claim coordination across a fleet of
// consumers.
//
// Registers its own topic (destroyed on exit), then runs three Consumers in
// one group and watches live worker_instance rows. The maintenance workers
// on the group's chain (janitor, cursor advancer) are target-1 rows: EXACTLY one
// live instance each no matter how many processes reconcile them -- the
// claim gate arbitrates, not leader election. The message_consumer row is
// the deliberate contrast: its target is unbounded, so every process runs
// its own consume loop.
//
// Confirms: three consumers -> one live janitor/cursor-advancer instance (never
// three) beside three consume loops; killing two, the survivor's manager
// re-claims whatever they held within a reconcile tick; killing the last,
// every claim releases and nothing stays live -- and the workers actually
// did their jobs (committed reached head).
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumer"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const (
	group       = "workerclaimlab"
	consumers   = 3
	seedRows    = 200
	instanceTTL = 2 * time.Second
)

// the target-1 rows on the group's chain; the manager rows are unbounded by
// design, and the system's cron_scheduler is shared state outside this
// lab's topic
var exclusive = []string{"topic_janitor", "cursor_advancer"}

func main() {
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("workerclaimlab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)
	for range seedRows {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
	head := scalar(ctx, ds, fmt.Sprintf(`SELECT COALESCE(max(id),0) FROM message_log_%d`, tp.Id))
	fmt.Printf("topic=%q id=%d seeded head=%d, instance ttl=%s, %d consumers\n", topicName, tp.Id, head, instanceTTL, consumers)

	running := make([]*runningConsumer, 0, consumers)
	for i := range consumers {
		running = append(running, start(ctx, ds, tp.Name, i))
	}
	groupId := scalar(ctx, ds, `SELECT id FROM consumer_group WHERE topic_id=$1 AND name=$2`, tp.Id, group)

	// ===== phase 1: N consumers, one live instance per target-1 row =====
	step("PHASE 1: 3 consumers for 8s -- janitor/cursor-advancer hold exactly 1 live instance, never 3")
	maxLive, live := sampleLive(ctx, ds, tp.Id, groupId, 8*time.Second)
	for _, name := range exclusive {
		fmt.Printf("  %-18s live=%d max seen=%d\n", name, live[name], maxLive[name])
		assertInt(name+" never exceeded its target", int64(maxLive[name]), 1)
		assertInt(name+" is claimed", int64(live[name]), 1)
	}
	fmt.Printf("  %-18s live=%d\n", "message_consumer", live["message_consumer"])
	assertInt("message_consumer runs unbounded -- one loop per process", int64(live["message_consumer"]), consumers)

	// ===== phase 2: kill two consumers, the survivor re-claims =====
	step("PHASE 2: kill 2 of 3 -- the survivor's manager re-claims whatever they held")
	for _, rc := range running[:2] {
		rc.stop()
	}
	maxLive, live = sampleLive(ctx, ds, tp.Id, groupId, 6*time.Second)
	for _, name := range exclusive {
		fmt.Printf("  %-18s live=%d max seen=%d\n", name, live[name], maxLive[name])
		assertInt(name+" still never exceeded its target", int64(maxLive[name]), 1)
		assertInt(name+" re-claimed after failover", int64(live[name]), 1)
	}
	assertInt("one consume loop left", int64(live["message_consumer"]), 1)

	// ===== phase 3: kill the last -- every claim releases =====
	step("PHASE 3: kill the survivor -- claims release, nothing stays live")
	running[2].stop()
	time.Sleep(instanceTTL) // a released row is gone at once; the TTL wait covers a claim mid-renewal
	_, live = sampleLive(ctx, ds, tp.Id, groupId, 2*time.Second)
	for _, name := range append(exclusive, "message_consumer") {
		fmt.Printf("  %-18s live=%d\n", name, live[name])
		assertInt(name+" has no live instance", int64(live[name]), 0)
	}

	// ===== the workers actually did their jobs =====
	step("END STATE: the coordinated workers did real work")
	assertInt("committed reached head", scalar(ctx, ds, fmt.Sprintf(`SELECT c.committed FROM cursor_%d c JOIN consumer_group g ON g.id = c.consumer_group_id WHERE g.name=$1`, tp.Id), group), head)

	fmt.Println("\n✅ WORKER CLAIM LAB PASSED")
	fmt.Println("   3 consumers -> one live instance per target-1 worker row, failover to the")
	fmt.Println("   survivor within a reconcile tick, full release when the last one exits.")
}

// runningConsumer is one Consumer's lifecycle handle: cancel stops it, done
// yields Consume's return.
type runningConsumer struct {
	cancel context.CancelFunc
	done   chan error
}

func (rc *runningConsumer) stop() {
	rc.cancel()
	must(<-rc.done)
}

func start(ctx context.Context, ds *iDatastore.PostgresDatastore, topicName string, i int) *runningConsumer {
	c, err := consumer.NewConsumer[common.Work](ds, &consumer.ConsumerConfig{
		BatchLimit:         50,
		QueueSize:          64,
		MessageConcurrency: 4,
		ClaimPollRate:      100 * time.Millisecond,
		InstanceTTL:        instanceTTL,
	})
	must(err)

	lifecycleCtx, cancel := context.WithCancel(ctx)
	cInstance, err := c.Register(lifecycleCtx, group, topicName, topic.SchemaVersion(1), nil)
	must(err)

	done := make(chan error, 1)
	go func() {
		err := cInstance.Consume(lifecycleCtx, func(ctx context.Context, work *common.Work) error { return nil })
		if err != nil && lifecycleCtx.Err() == nil {
			fmt.Printf("  consumer %d died early: %v\n", i, err)
		}
		done <- err
	}()
	fmt.Printf("  consumer %d started\n", i)
	return &runningConsumer{cancel: cancel, done: done}
}

// sampleLive polls the chain's worker rows for dur and tracks live instance
// counts per row: the last count seen and the highest ever seen -- the max
// is what proves the claim gate held while processes fought over it.
func sampleLive(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, dur time.Duration) (map[string]int, map[string]int) {
	maxLive := map[string]int{}
	live := map[string]int{}
	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		rows, err := ds.Pool.Query(ctx, `
			SELECT w.name, COUNT(i.id) FILTER (WHERE i.expires_at > now())
			FROM worker w
			LEFT JOIN worker_instance i ON i.worker_id = w.id
			WHERE w.topic_id = $1
				OR w.consumer_group_id = $2
			GROUP BY w.name`, topicId, groupId)
		must(err)
		for rows.Next() {
			var name string
			var n int
			must(rows.Scan(&name, &n))
			live[name] = n
			if n > maxLive[name] {
				maxLive[name] = n
			}
		}
		must(rows.Err())
		rows.Close()
		time.Sleep(50 * time.Millisecond)
	}
	return maxLive, live
}

// ---- helpers ----

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
	fmt.Println("❌ " + msg)
	os.Exit(1)
}

func assertInt(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
