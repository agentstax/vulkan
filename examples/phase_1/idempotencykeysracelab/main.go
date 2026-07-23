package main

// idempotency_key concurrency lab: the permanent regression counterpart to
// the throwaway pgxpool test that verified this during the idempotency_key
// per-topic redesign -- every other idempotency lab only ever publishes
// sequentially, so the claim+insert CTE's true concurrent behavior (as
// opposed to sequential "retries") has never been exercised as a standing
// test. Mirrors latestkeysracelab's concurrent-race precedent.
//
// Two scenarios:
//   - sameKeyConcurrentScenario: N goroutines publish under the SAME
//     idempotency key at once -- exactly 1 must land, regardless of which
//     goroutine's claim insert happened to commit first.
//   - distinctKeysConcurrentScenario: N goroutines each publish under their
//     OWN distinct key, all at once -- every one must land; concurrency
//     alone must never cause a spurious collision or a lost write across
//     unrelated keys.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
		MaxConns: 60, // headroom above both scenarios' 50 concurrent publishers
	})
	must(err)
	defer ds.Close()

	sameKeyConcurrentScenario(ctx, ds)
	distinctKeysConcurrentScenario(ctx, ds)

	fmt.Println("\n✅ IDEMPOTENCY KEYS RACE LAB PASSED")
	fmt.Println("   N concurrent publishes under one shared key land exactly once, and N")
	fmt.Println("   concurrent publishes under N distinct keys all land -- the claim+insert")
	fmt.Println("   CTE holds up under true concurrency, not just sequential retries.")
}

// sameKeyConcurrentScenario: N goroutines share ONE idempotency key and
// publish at the exact same time -- exactly 1 message and 1 claim row must
// land, however the goroutines' claim inserts happen to interleave/commit.
func sameKeyConcurrentScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("same key, concurrent: N goroutines sharing one idempotency key must land exactly once")

	const n = 50
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("phase9.idempotencykeysracelab.same.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topic.Config{PartitionSize: 1000})
	must(err)
	defer func() { must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true})) }()

	wp, err := producer.NewMessageProducer[common.Work](tp.Name, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	key := uuid.Must(uuid.NewV7())

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
				return common.NewWork(30, "admin@example.com")
			}, producer.ProduceOptions{IdempotencyKey: key})
			must(err)
		})
	}
	wg.Wait()

	assertCount(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), 1, fmt.Sprintf("%d concurrent publishes under one shared key landed exactly 1 message", n))
	assertCount(ctx, ds, fmt.Sprintf("idempotency_key_%d", tp.Id), 1, fmt.Sprintf("%d concurrent publishes under one shared key left exactly 1 claim row", n))

	var exists bool
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM idempotency_key_%d WHERE idempotency_key = $1);`, tp.Id), key).Scan(&exists))
	if !exists {
		die("the one surviving claim row is not keyed to the idempotency key every goroutine shared")
	}
	fmt.Println("  ✓ the surviving claim row is keyed to the shared idempotency key")
}

// distinctKeysConcurrentScenario: N goroutines each publish under their OWN
// distinct key, all at once -- concurrency alone must never drop a write or
// cause a false collision across unrelated keys.
func distinctKeysConcurrentScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("distinct keys, concurrent: N goroutines each with their own key must all land")

	const n = 50
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("phase9.idempotencykeysracelab.distinct.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topic.Config{PartitionSize: 1000})
	must(err)
	defer func() { must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true})) }()

	wp, err := producer.NewMessageProducer[common.Work](tp.Name, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			key := uuid.Must(uuid.NewV7())
			_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
				return common.NewWork(30, "admin@example.com")
			}, producer.ProduceOptions{IdempotencyKey: key})
			must(err)
		})
	}
	wg.Wait()

	assertCount(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), n, fmt.Sprintf("%d concurrent publishes under %d distinct keys all landed", n, n))
	assertCount(ctx, ds, fmt.Sprintf("idempotency_key_%d", tp.Id), n, fmt.Sprintf("%d concurrent publishes under %d distinct keys left %d distinct claim rows", n, n, n))
}

// ---- helpers ----

func assertCount(ctx context.Context, ds *coredatastore.PostgresDatastore, table string, want int, label string) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s;`, table)).Scan(&count))
	if count != want {
		die(fmt.Sprintf("%s: %s has %d rows, want %d", label, table, count, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, count)
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
