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
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/system"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

// every table createSystemTables creates -- the teardown assertion list
var controlPlaneTables = []string{
	"system_config",
	"topic_config",
	"topic_config_log",
	"consumer_group_config",
	"worker_config",
	"worker_config_log",
	"worker_instance",
	"schedule_config",
	"schedule_cursor",
	"migration_log",
}

var ds *iDatastore.PostgresDatastore

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

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds = client.Datastore()
	must(client.System().Register(ctx, nil))

	step("seed a user topic with messages")
	topicName := fmt.Sprintf("destroysystemlab.%d", time.Now().UnixNano())
	tp, err := client.Topic[vulkan.RawPayload](topicName).Register(ctx, nil)
	must(err)
	wpInstance, err := client.Topic[common.Work](tp.Name).Producer().Register(ctx, nil)
	must(err)
	for range 3 {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, nil)
		must(err)
	}

	step("a registered user topic refuses the destroy")
	err = client.System().Destroy(ctx, nil)
	assertErrorIs("ErrTopicsRegistered", err, system.ErrTopicsRegistered)

	step("a running consumer refuses it first -- the worker guard outranks the topic guard")
	wcInstance, err := client.Topic[common.Work](tp.Name).Group("destroysystemlab-group").Register(ctx, nil)
	must(err)
	consumeCtx, stopConsumer := context.WithCancel(ctx)
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- wcInstance.Consume(consumeCtx, func(ctx context.Context, work *common.Work) error {
			return nil
		}, &vulkan.ConsumeOptions{
			ClaimPollRate: 500 * time.Millisecond,
			InstanceTTL:   2 * time.Second,
		})
	}()
	waitLiveInstances(ctx, true)

	err = client.System().Destroy(ctx, nil)
	assertErrorIs("ErrSystemLive", err, system.ErrSystemLive)

	stopConsumer()
	must(<-consumeDone)
	waitLiveInstances(ctx, false)

	step("consumer stopped: the topic guard is back")
	err = client.System().Destroy(ctx, nil)
	assertErrorIs("ErrTopicsRegistered", err, system.ErrTopicsRegistered)

	step("user topic destroyed: the unforced destroy succeeds")
	// a system topic's id, so the teardown assert can cover a physical
	// table the destroy itself must drop (not one DestroyTopic already took)
	var alertsTopicId int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s.topic_config WHERE name = '__system.alerts';`, ds.Schema)).Scan(&alertsTopicId))

	must(client.Topic[vulkan.RawPayload](topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	must(client.System().Destroy(ctx, nil))

	for _, table := range controlPlaneTables {
		assertTableExists(ctx, ds.Schema+"."+table, false)
	}
	assertTableExists(ctx, fmt.Sprintf("%s.%s", ds.Schema, topic.MessageLogTable(alertsTopicId)), false)

	step("a second destroy is a no-op, not an error")
	must(client.System().Destroy(ctx, nil))
	fmt.Println("  ✓ destroy of an already-destroyed system returned nil")

	step("RegisterSystem stands the schema back up")
	must(client.System().Register(ctx, nil))
	for _, table := range controlPlaneTables {
		assertTableExists(ctx, ds.Schema+"."+table, true)
	}
	var topicCount int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.topic_config;`, ds.Schema)).Scan(&topicCount))
	assertTrue(fmt.Sprintf("the 3 system topics re-registered (got %d)", topicCount), topicCount == 3)

	fmt.Println("\n✅ DESTROY SYSTEM LAB PASSED")
	fmt.Println("   guards refuse while workers run or topics remain; the unforced destroy")
	fmt.Println("   returns the database to its pre-register state, and RegisterSystem rebuilds it.")
	return nil
}

// ---- helpers ----

// waitLiveInstances polls until some worker_instance row is live (want=true)
// or every row is gone or expired (want=false). Released instances delete
// their rows; a crashed one lingers only until the lab's 2s InstanceTTL.
func waitLiveInstances(ctx context.Context, want bool) {
	deadline := time.Now().Add(30 * time.Second)
	for {
		var live bool
		must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
			SELECT EXISTS (SELECT 1 FROM %s.worker_instance WHERE expires_at > now());
		`, ds.Schema)).Scan(&live))
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
	panic(labFailure{message: msg})
}
