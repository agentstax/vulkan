// Command maintenancelab proves the maintenance tier's claim coordination
// across a fleet of consumers.
//
// Registers its own topic (destroyed on exit) with 1s janitor/waterline rates,
// then runs three Consumers in one group and counts duty EXECUTIONS by watching
// the maintenance rows' fencing tokens: every claim that wins rotates the
// token, losers don't touch it, and renewal heartbeats keep the token stable --
// so token changes count effective workers, not claim attempts.
//
// Confirms: three consumers produce ~one execution per duty per interval (not
// three); killing two, the survivor keeps both duties running within an
// interval (failover); killing the last, executions stop entirely -- and the
// duties actually worked (waterline reached head, janitor created ahead).
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const (
	group     = "maintenancelab"
	dutyRate  = time.Second
	consumers = 3
	seedRows  = 200
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("maintenancelab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	// speed the janitor up to the lab's 1s tick (the waterline's seeded
	// default is already 1s) -- rates live on the maintenance row's metadata,
	// jsonb_set so the row's other tuning (sweep_batch_size) survives
	_, err = ds.Pool.Exec(ctx,
		`UPDATE maintenance SET metadata = jsonb_set(metadata, '{poll_rate}', to_jsonb($1::bigint)) WHERE duty = 'janitor' AND topic_id = $2;`,
		int64(dutyRate), tp.Id)
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	wp, err := producer.NewProducer[common.Work](tp.Name, topic.SchemaVersion(1), ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	wpInstance, err := wp.Register(ctx)
	must(err)
	for range seedRows {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
	head := scalar(ctx, ds, fmt.Sprintf(`SELECT COALESCE(max(id),0) FROM message_log_%d`, tp.Id))
	fmt.Printf("topic=%q id=%d seeded head=%d, duty rate=%s, %d consumers\n", topicName, tp.Id, head, dutyRate, consumers)

	running := make([]*runningConsumer, 0, consumers)
	for i := range consumers {
		running = append(running, start(ctx, ds, tp.Name, i))
	}

	// ===== phase 1: N consumers, one effective worker per duty interval =====
	step("PHASE 1: 3 consumers for 10s -- expect ~1 execution per duty per interval, not 3")
	counts := sampleRotations(ctx, ds, tp.Id, 10*time.Second)
	for _, duty := range []string{"janitor", "waterline"} {
		fmt.Printf("  %-9s executions=%d (independent workers would be ~%d)\n", duty, counts[duty], 3*10)
		assertBetween(duty+" executions ~ one per interval", counts[duty], 6, 14)
	}

	// ===== phase 2: kill two consumers, the survivor carries both duties =====
	step("PHASE 2: kill 2 of 3 -- survivor keeps both duties running")
	for _, rc := range running[:2] {
		rc.stop()
	}
	counts = sampleRotations(ctx, ds, tp.Id, 6*time.Second)
	for _, duty := range []string{"janitor", "waterline"} {
		fmt.Printf("  %-9s executions=%d\n", duty, counts[duty])
		assertBetween(duty+" kept running after failover", counts[duty], 3, 9)
	}

	// ===== phase 3: kill the last -- executions stop =====
	step("PHASE 3: kill the survivor -- duty executions stop")
	running[2].stop()
	// the final claim keeps the gate up to one rate in the future; let it pass
	// before asserting silence
	time.Sleep(2 * dutyRate)
	counts = sampleRotations(ctx, ds, tp.Id, 3*time.Second)
	for _, duty := range []string{"janitor", "waterline"} {
		fmt.Printf("  %-9s executions=%d\n", duty, counts[duty])
		assertBetween(duty+" silent with no consumers", counts[duty], 0, 0)
	}

	// ===== the duties actually did their jobs =====
	step("END STATE: the coordinated duties did real work")
	assertInt("waterline reached head", scalar(ctx, ds, `SELECT c.committed FROM cursor c JOIN consumer_group g ON g.id = c.consumer_group_id WHERE g.name=$1 AND g.topic_id=$2`, group, tp.Id), head)
	partitions := scalar(ctx, ds, fmt.Sprintf(`
		SELECT count(*) FROM pg_inherits i
		JOIN pg_class c ON c.oid = i.inhrelid
		WHERE i.inhparent = 'message_log_%d'::regclass;
	`, tp.Id))
	fmt.Printf("  partitions=%d\n", partitions)
	if partitions < 2 {
		die("expected the janitor's create-ahead partition (>= 2 partitions)")
	}

	fmt.Println("\n✅ MAINTENANCE LAB PASSED")
	fmt.Println("   3 consumers -> one effective janitor/waterline worker per interval,")
	fmt.Println("   failover to the survivor within an interval, full stop when the last one exits.")
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

func start(ctx context.Context, ds *coredatastore.PostgresDatastore, topicName string, i int) *runningConsumer {
	c, err := consumer.NewConsumer[common.Work](group, topicName, topic.SchemaVersion(1), ds, &consumer.ConsumerConfig{
		BatchLimit:         50,
		QueueSize:          64,
		MessageConcurrency: 4,
		ClaimPollRate:      100 * time.Millisecond,
	})
	must(err)

	lifecycleCtx, cancel := context.WithCancel(ctx)
	cInstance, err := c.Register(lifecycleCtx)
	must(err)

	done := make(chan error, 1)
	go func() {
		done <- cInstance.Consume(lifecycleCtx, func(ctx context.Context, work *common.Work) error { return nil })
	}()
	fmt.Printf("  consumer %d started\n", i)
	return &runningConsumer{cancel: cancel, done: done}
}

// sampleRotations polls the topic's maintenance rows for dur and counts token
// changes per duty. A change = one granted claim = one duty execution (renew
// heartbeats fence on the token without rotating it).
func sampleRotations(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, dur time.Duration) map[string]int {
	counts := map[string]int{}
	last := map[string]string{}
	deadline := time.Now().Add(dur)
	first := true
	for time.Now().Before(deadline) {
		rows, err := ds.Pool.Query(ctx, `
			SELECT m.duty, COALESCE(m.token::text, '')
			FROM maintenance m
			LEFT JOIN consumer_group g ON g.id = m.consumer_group_id
			WHERE COALESCE(m.topic_id, g.topic_id) = $1`, topicID)
		must(err)
		for rows.Next() {
			var duty, token string
			must(rows.Scan(&duty, &token))
			if !first && token != last[duty] {
				counts[duty]++
			}
			last[duty] = token
		}
		must(rows.Err())
		rows.Close()
		first = false
		time.Sleep(25 * time.Millisecond)
	}
	return counts
}

// ---- helpers ----

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
	fmt.Println("❌ " + msg)
	os.Exit(1)
}

func assertInt(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}

func assertBetween(label string, got, low, high int) {
	if got < low || got > high {
		die(fmt.Sprintf("%s: got %d, want [%d, %d]", label, got, low, high))
	}
	fmt.Printf("  ✓ %s (%d in [%d, %d])\n", label, got, low, high)
}
