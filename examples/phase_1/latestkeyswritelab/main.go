package main

// latest_keys write-cost lab: quantifies the tradeoff this phase's design
// made deliberately but never measured -- read-path scans got O(1), at the
// cost of a second write (an UPSERT into latest_keys, same transaction) on
// every keyed publish. Three scenarios:
//
//   - Fixed cost: sequential, uncontended publishes -- unkeyed vs. a fresh
//     key each time (pure INSERT into latest_keys) vs. the SAME key every
//     time (the ON CONFLICT DO UPDATE branch). Isolates the extra
//     statement's own cost from any lock contention.
//   - Hot-key contention: G goroutines concurrently publish -- each to its
//     OWN distinct key (parallel latest_keys rows, no contention) vs. all G
//     to the SAME single key (serialized on that one row, the "known
//     tradeoff" flagged back in the design but never measured under load).
//   - Dead-tuple growth: the hot-key scenario repeatedly UPDATEs ONE row --
//     n_dead_tup/n_tup_upd on latest_keys before and after the burst shows
//     what that does to table bloat, separate from the latency question.
//
// Registers its own topics (destroyed on exit), self-seeded, self-verifying.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const largePartitionSize = int64(1000000) // never rolls -- partition churn isn't what's being measured

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
		MaxConns: 60, // headroom above the hot-key scenario's 50 concurrent goroutines
	})
	must(err)

	fixedCostScenario(ctx, ds)
	hotKeyContentionScenario(ctx, ds)

	fmt.Println("\n✅ LATEST KEYS WRITE-COST LAB — numbers gathered, see LEARNING_PLAN.md's 8c")
	fmt.Println("   \"Known tradeoff\" bullet for what they mean and whether they change anything.")
}

// fixedCostScenario: N sequential, single-threaded publishes per case --
// zero contention, so the only thing the timing difference can reflect is
// the extra statement itself (and INSERT vs. UPDATE within it).
func fixedCostScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("fixed cost: sequential publishes, no contention -- unkeyed vs. fresh-key INSERT vs. same-key UPDATE")

	const n = 500
	topicName := fmt.Sprintf("phase8c.latestkeyswritelab.fixed.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: largePartitionSize})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	pd := producer.NewProducerDatastore[common.Work](ds)
	wp := producer.NewWorkProducer(tp, pd)

	unkeyedMs := timeSequential(ctx, wp, n, func(i int) string { return "" })
	freshKeyMs := timeSequential(ctx, wp, n, func(i int) string { return fmt.Sprintf("fresh-%d", i) })
	sameKeyMs := timeSequential(ctx, wp, n, func(i int) string { return "same-key" })

	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op\n", "unkeyed (baseline)", unkeyedMs, unkeyedMs/n)
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op  (+%.1f%% vs. baseline)\n", "fresh key (latest_keys INSERT)", freshKeyMs, freshKeyMs/n, pctOver(freshKeyMs, unkeyedMs))
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op  (+%.1f%% vs. baseline)\n", "same key (latest_keys UPDATE)", sameKeyMs, sameKeyMs/n, pctOver(sameKeyMs, unkeyedMs))
}

// hotKeyContentionScenario: the design's own flagged-but-unmeasured tradeoff
// -- concurrent publishes to the SAME key now serialize on that key's
// latest_keys row, where plain message_log appends never contended before.
func hotKeyContentionScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("hot-key contention: G concurrent publishers, each to its OWN key vs. all G to ONE key")

	const goroutines = 50
	const perGoroutine = 20

	manyKeysMs, manyKeysTopic := timeConcurrent(ctx, ds, "manykeys", goroutines, perGoroutine, func(g, i int) string {
		return fmt.Sprintf("key-%d", g) // each goroutine owns a distinct key -- no cross-goroutine contention
	})
	defer func() { must(topic.Destroy(ctx, ds, manyKeysTopic)) }()

	before := dumpTableStats(ctx, ds, "latest_keys")

	oneKeyMs, oneKeyTopic := timeConcurrent(ctx, ds, "onekey", goroutines, perGoroutine, func(g, i int) string {
		return "hot-key" // every goroutine hammers the SAME row
	})
	defer func() { must(topic.Destroy(ctx, ds, oneKeyTopic)) }()

	time.Sleep(1 * time.Second) // let PG's stats collector flush before reading it
	after := dumpTableStats(ctx, ds, "latest_keys")

	total := goroutines * perGoroutine
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op (%d ops, %d goroutines)\n", "many distinct keys", manyKeysMs, manyKeysMs/float64(total), total, goroutines)
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op (%d ops, %d goroutines)\n", "one hot key", oneKeyMs, oneKeyMs/float64(total), total, goroutines)
	fmt.Printf("  -> %.1fx slower under full serialization on a single key\n", oneKeyMs/manyKeysMs)

	step("dead-tuple growth from the hot-key burst")
	fmt.Printf("  before: n_live_tup=%d n_dead_tup=%d n_tup_upd=%d\n", before.liveTup, before.deadTup, before.tupUpd)
	fmt.Printf("  after:  n_live_tup=%d n_dead_tup=%d n_tup_upd=%d\n", after.liveTup, after.deadTup, after.tupUpd)
	fmt.Printf("  -> %d updates against ONE row produced %d new dead tuples, pending autovacuum\n",
		after.tupUpd-before.tupUpd, after.deadTup-before.deadTup)
}

// ---- helpers ----

// timeSequential runs n single-threaded publishes, keyFn(i) chosen per call,
// returning total elapsed time in milliseconds.
func timeSequential(ctx context.Context, wp *producer.WorkProducer[common.Work], n int, keyFn func(i int) string) float64 {
	start := time.Now()
	for i := range n {
		_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{CompactionKey: keyFn(i)})
		must(err)
	}
	return float64(time.Since(start).Microseconds()) / 1000.0
}

// timeConcurrent registers its own topic, fires goroutines*perGoroutine
// publishes across `goroutines` concurrent workers, and returns total
// elapsed time plus the topic name (caller destroys it once done reading it).
func timeConcurrent(ctx context.Context, ds *coredatastore.PostgresDatastore, label string, goroutines, perGoroutine int, keyFn func(g, i int) string) (float64, string) {
	name := fmt.Sprintf("phase8c.latestkeyswritelab.%s.%d", label, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: name, PartitionSize: largePartitionSize})
	must(err)

	pd := producer.NewProducerDatastore[common.Work](ds)
	wp := producer.NewWorkProducer(tp, pd)

	start := time.Now()
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := range perGoroutine {
				_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
					return common.NewWork(30, "admin@example.com")
				}, producer.ProduceOptions{CompactionKey: keyFn(g, i)})
				must(err)
			}
		})
	}
	wg.Wait()
	elapsedMs := float64(time.Since(start).Microseconds()) / 1000.0

	return elapsedMs, name
}

type tableStats struct {
	liveTup int64
	deadTup int64
	tupUpd  int64
}

func dumpTableStats(ctx context.Context, ds *coredatastore.PostgresDatastore, table string) tableStats {
	var s tableStats
	sql := `
		SELECT n_live_tup, n_dead_tup, n_tup_upd
		FROM pg_stat_user_tables
		WHERE relname = $1;
	`
	must(ds.Pool.QueryRow(ctx, sql, table).Scan(&s.liveTup, &s.deadTup, &s.tupUpd))
	return s
}

func pctOver(got, baseline float64) float64 {
	if baseline == 0 {
		return 0
	}
	return (got - baseline) / baseline * 100
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
