package main

// idempotency_key concurrency lab: the permanent regression counterpart to
// the throwaway pgxpool test that verified this during the idempotency_key
// per-topic redesign -- every other idempotency lab only ever publishes
// sequentially, so the claim+insert CTE's true concurrent behavior (as
// opposed to sequential "retries") has never been exercised as a standing
// test. Mirrors compactionheadracelab's concurrent-race precedent.
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
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"uuid"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/jackc/pgx/v5/pgxpool"
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

	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{
		MaxConns: 60, // headroom above both scenarios' 50 concurrent publishers
	})
	must(err)
	defer pool.Close()

	sameKeyConcurrentScenario(ctx, pool)
	distinctKeysConcurrentScenario(ctx, pool)

	fmt.Println("\n✅ IDEMPOTENCY KEYS RACE LAB PASSED")
	fmt.Println("   N concurrent publishes under one shared key land exactly once, and N")
	fmt.Println("   concurrent publishes under N distinct keys all land -- the claim+insert")
	fmt.Println("   CTE holds up under true concurrency, not just sequential retries.")
	return nil
}

// sameKeyConcurrentScenario: N goroutines share ONE idempotency key and
// publish at the exact same time -- exactly 1 message and 1 claim row must
// land, however the goroutines' claim inserts happen to interleave/commit.
func sameKeyConcurrentScenario(ctx context.Context, pool *pgxpool.Pool) {
	step("same key, concurrent: N goroutines sharing one idempotency key must land exactly once")

	const n = 50
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	ds := client.Datastore()

	topicName := fmt.Sprintf("phase9.idempotencykeysracelab.same.%d", time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{PartitionSize: 1000})
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)

	key := uuid.NewV7().String()

	var wg sync.WaitGroup
	var duplicateCount atomic.Int64
	for range n {
		wg.Go(func() {
			produced, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx, _ string) (*common.Work, error) {
				return common.NewWork(30, "admin@example.com")
			}, &vulkan.ProduceOptions{IdempotencyKey: key})
			must(err)
			if produced.Duplicate {
				duplicateCount.Add(1)
			}
		})
	}
	wg.Wait()

	if duplicateCount.Load() != n-1 {
		die(fmt.Sprintf("%d of %d calls reported Duplicate, want %d -- exactly 1 winner", duplicateCount.Load(), n, n-1))
	}
	fmt.Printf("  ✓ exactly 1 of %d concurrent calls stored the message, %d reported Duplicate\n", n, n-1)
	assertCount(ctx, ds, fmt.Sprintf("%s.%s", ds.Schema, topic.MessageLogTable(tp.Id)), 1, fmt.Sprintf("%d concurrent publishes under one shared key landed exactly 1 message", n))
	assertCount(ctx, ds, fmt.Sprintf("%s.%s", ds.Schema, topic.IdempotencyKeyTable(tp.Id)), 1, fmt.Sprintf("%d concurrent publishes under one shared key left exactly 1 claim row", n))

	var exists bool
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT EXISTS(SELECT 1 FROM %s.%s WHERE idempotency_key = $1);`, ds.Schema, topic.IdempotencyKeyTable(tp.Id)), key).Scan(&exists))
	if !exists {
		die("the one surviving claim row is not keyed to the idempotency key every goroutine shared")
	}
	fmt.Println("  ✓ the surviving claim row is keyed to the shared idempotency key")
}

// distinctKeysConcurrentScenario: N goroutines each publish under their OWN
// distinct key, all at once -- concurrency alone must never drop a write or
// cause a false collision across unrelated keys.
func distinctKeysConcurrentScenario(ctx context.Context, pool *pgxpool.Pool) {
	step("distinct keys, concurrent: N goroutines each with their own key must all land")

	const n = 50
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	ds := client.Datastore()

	topicName := fmt.Sprintf("phase9.idempotencykeysracelab.distinct.%d", time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{PartitionSize: 1000})
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)

	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			key := uuid.NewV7().String()
			_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx, _ string) (*common.Work, error) {
				return common.NewWork(30, "admin@example.com")
			}, &vulkan.ProduceOptions{IdempotencyKey: key})
			must(err)
		})
	}
	wg.Wait()

	assertCount(ctx, ds, fmt.Sprintf("%s.%s", ds.Schema, topic.MessageLogTable(tp.Id)), n, fmt.Sprintf("%d concurrent publishes under %d distinct keys all landed", n, n))
	assertCount(ctx, ds, fmt.Sprintf("%s.%s", ds.Schema, topic.IdempotencyKeyTable(tp.Id)), n, fmt.Sprintf("%d concurrent publishes under %d distinct keys left %d distinct claim rows", n, n, n))
}

// ---- helpers ----

func assertCount(ctx context.Context, ds *iDatastore.PostgresDatastore, table string, want int, label string) {
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
	panic(labFailure{message: msg})
}
