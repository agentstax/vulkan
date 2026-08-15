package main

// destroy-system lab: DestroySystem is RegisterSystem's inverse (decision
// record [0514]). Walks the verb through its guards and its teardown:
//
//   - a registered user topic refuses the destroy (ErrTopicsRegistered)
//   - a running consumer refuses it first (ErrSystemLive) -- the worker
//     guard outranks the topic guard
//   - with the consumer stopped and the user topic destroyed, the unforced
//     destroy succeeds: every control-plane table and every system topic's
//     physical tables are gone; a second destroy is a no-op (idempotent)
//   - RegisterSystem stands the schema back up, leaving the database usable
//
// Self-seeded, self-verifying; ends with the system re-registered.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/system"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

// every table createSystemTables creates -- the teardown assertion list
var controlPlaneTables = []string{
	"system",
	"topic",
	"consumer_group",
	"cursor",
	"lease",
	"key_lease",
	"worker",
	"worker_instance",
	"binding",
	"binding_declaration",
	"compaction_head",
	"cron_job",
	"migration_log",
}

var ds *coredatastore.PostgresDatastore

func main() {
	ctx := context.Background()

	var err error
	ds, err = coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	step("seed a user topic with messages")
	topicName := fmt.Sprintf("destroysystemlab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), nil)
	must(err)
	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)
	for range 3 {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}

	step("a registered user topic refuses the destroy")
	err = mAdmin.DestroySystem(ctx, admin.DestroyOptions{})
	assertErrorIs("ErrTopicsRegistered", err, system.ErrTopicsRegistered)

	step("a running consumer refuses it first -- the worker guard outranks the topic guard")
	wc, err := consumer.NewConsumer[common.Work](ds, &consumer.ConsumerConfig{
		ClaimPollRate: 500 * time.Millisecond,
		InstanceTTL:   2 * time.Second,
	})
	must(err)
	wcInstance, err := wc.Register(ctx, "destroysystemlab-group", tp.Name, topic.SchemaVersion(1), nil)
	must(err)
	consumeCtx, stopConsumer := context.WithCancel(ctx)
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- wcInstance.Consume(consumeCtx, func(ctx context.Context, work *common.Work) error {
			return nil
		})
	}()
	waitLiveInstances(ctx, true)

	err = mAdmin.DestroySystem(ctx, admin.DestroyOptions{})
	assertErrorIs("ErrSystemLive", err, system.ErrSystemLive)

	stopConsumer()
	must(<-consumeDone)
	waitLiveInstances(ctx, false)

	step("consumer stopped: the topic guard is back")
	err = mAdmin.DestroySystem(ctx, admin.DestroyOptions{})
	assertErrorIs("ErrTopicsRegistered", err, system.ErrTopicsRegistered)

	step("user topic destroyed: the unforced destroy succeeds")
	// a system topic's id, so the teardown assert can cover a physical
	// table the destroy itself must drop (not one DestroyTopic already took)
	var alertsTopicId int64
	must(ds.Pool.QueryRow(ctx, `SELECT id FROM topic WHERE name = '__system.alerts';`).Scan(&alertsTopicId))

	must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	must(mAdmin.DestroySystem(ctx, admin.DestroyOptions{}))

	for _, table := range controlPlaneTables {
		assertTableExists(ctx, table, false)
	}
	assertTableExists(ctx, fmt.Sprintf("message_log_%d", alertsTopicId), false)

	step("a second destroy is a no-op, not an error")
	must(mAdmin.DestroySystem(ctx, admin.DestroyOptions{}))
	fmt.Println("  ✓ destroy of an already-destroyed system returned nil")

	step("RegisterSystem stands the schema back up")
	must(mAdmin.RegisterSystem(ctx, nil))
	for _, table := range controlPlaneTables {
		assertTableExists(ctx, table, true)
	}
	var topicCount int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM topic;`).Scan(&topicCount))
	assertTrue(fmt.Sprintf("the 3 system topics re-registered (got %d)", topicCount), topicCount == 3)

	fmt.Println("\n✅ DESTROY SYSTEM LAB PASSED")
	fmt.Println("   guards refuse while workers run or topics remain; the unforced destroy")
	fmt.Println("   returns the database to its pre-register state, and RegisterSystem rebuilds it.")
}

// ---- helpers ----

// waitLiveInstances polls until some worker_instance row is live (want=true)
// or every row is gone or expired (want=false). Released instances delete
// their rows; a crashed one lingers only until the lab's 2s InstanceTTL.
func waitLiveInstances(ctx context.Context, want bool) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var live bool
		must(ds.Pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM worker_instance WHERE expires_at > now());
		`).Scan(&live))
		if live == want {
			return
		}
		if time.Now().After(deadline) {
			die(fmt.Sprintf("live worker instances never became %v", want))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func assertTableExists(ctx context.Context, table string, want bool) {
	var exists bool
	must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL;`, table).Scan(&exists))
	if exists != want {
		die(fmt.Sprintf("table %s exists=%v, want %v", table, exists, want))
	}
	fmt.Printf("  ✓ table %s exists=%v\n", table, exists)
}

func assertErrorIs(label string, err error, target error) {
	if !errors.Is(err, target) {
		die(fmt.Sprintf("%s: got %v", label, err))
	}
	fmt.Printf("  ✓ refused with %s: %v\n", label, err)
}

func assertTrue(label string, cond bool) {
	if !cond {
		die(fmt.Sprintf("%s: got false, want true", label))
	}
	fmt.Printf("  ✓ %s\n", label)
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
