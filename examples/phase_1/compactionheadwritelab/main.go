package main

// compaction_head write-cost lab: quantifies the tradeoff this phase's design
// made deliberately but never measured -- read-path scans got O(1), at the
// cost of a second write (an UPSERT into compaction_head, same transaction) on
// every keyed publish. Three scenarios:
//
//   - Fixed cost: sequential, uncontended publishes -- unkeyed vs. a fresh
//     key each time (pure INSERT into compaction_head) vs. the SAME key every
//     time (the ON CONFLICT DO UPDATE branch). Isolates the extra
//     statement's own cost from any lock contention.
//   - Hot-key contention: G goroutines concurrently publish -- each to its
//     OWN distinct key (parallel compaction_head rows, no contention) vs. all G
//     to the SAME single key (serialized on that one row, the "known
//     tradeoff" flagged back in the design but never measured under load).
//   - Dead-tuple growth: the hot-key scenario repeatedly UPDATEs ONE row --
//     n_dead_tup/n_tup_upd on compaction_head before and after the burst shows
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
	"github.com/agentstax/vulkan/pkg/admin"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const largePartitionSize = int64(1000000) // never rolls -- partition churn isn't what's being measured

func main() {
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{
		Pass:     "example_password",
		MaxConns: 60, // headroom above the hot-key scenario's 50 concurrent goroutines
	})
	must(err)
	defer ds.Close()

	fixedCostScenario(ctx, ds)
	hotKeyContentionScenario(ctx, ds)

	fmt.Println("\n✅ LATEST KEYS WRITE-COST LAB — numbers gathered; decision record [0262]")
	fmt.Println("   (docs/decisions/) holds the write-per-keyed-publish tradeoff they measure.")
}

// fixedCostScenario: N sequential, single-threaded publishes per case --
// zero contention, so the only thing the timing difference can reflect is
// the extra statement itself (and INSERT vs. UPDATE within it).
func fixedCostScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("fixed cost: sequential publishes, no contention -- unkeyed vs. fresh-key INSERT vs. same-key UPDATE")

	const n = 500
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("phase8c.compactionheadwritelab.fixed.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{PartitionSize: largePartitionSize})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)

	unkeyedMs := timeSequential(ctx, wpInstance, n, func(i int) string { return "" })
	freshKeyMs := timeSequential(ctx, wpInstance, n, func(i int) string { return fmt.Sprintf("fresh-%d", i) })
	sameKeyMs := timeSequential(ctx, wpInstance, n, func(i int) string { return "same-key" })

	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op\n", "unkeyed (baseline)", unkeyedMs, unkeyedMs/n)
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op  (+%.1f%% vs. baseline)\n", "fresh key (compaction_head INSERT)", freshKeyMs, freshKeyMs/n, pctOver(freshKeyMs, unkeyedMs))
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op  (+%.1f%% vs. baseline)\n", "same key (compaction_head UPDATE)", sameKeyMs, sameKeyMs/n, pctOver(sameKeyMs, unkeyedMs))
}

// hotKeyContentionScenario: the design's own flagged-but-unmeasured tradeoff
// -- concurrent publishes to the SAME key now serialize on that key's
// compaction_head row, where plain message_log appends never contended before.
func hotKeyContentionScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("hot-key contention: G concurrent publishers, each to its OWN key vs. all G to ONE key")

	const goroutines = 50
	const perGoroutine = 20

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	manyKeysMs, manyKeysTopic := timeConcurrent(ctx, ds, "manykeys", goroutines, perGoroutine, func(g, i int) string {
		return fmt.Sprintf("key-%d", g) // each goroutine owns a distinct key -- no cross-goroutine contention
	})
	defer func() {
		must(mAdmin.DestroyTopic(ctx, manyKeysTopic, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	oneKeyMs, oneKeyTopic := timeConcurrent(ctx, ds, "onekey", goroutines, perGoroutine, func(g, i int) string {
		return "hot-key" // every goroutine hammers the SAME row
	})
	defer func() {
		must(mAdmin.DestroyTopic(ctx, oneKeyTopic, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	time.Sleep(1 * time.Second) // let PG's stats collector flush before reading it
	// the one-hot-key topic's compaction_head table saw only the burst, so
	// its absolute stats are the burst's numbers
	stats := dumpTableStats(ctx, ds, compactionHeadTable(ctx, ds, oneKeyTopic))

	total := goroutines * perGoroutine
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op (%d ops, %d goroutines)\n", "many distinct keys", manyKeysMs, manyKeysMs/float64(total), total, goroutines)
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op (%d ops, %d goroutines)\n", "one hot key", oneKeyMs, oneKeyMs/float64(total), total, goroutines)
	fmt.Printf("  -> %.1fx slower under full serialization on a single key\n", oneKeyMs/manyKeysMs)

	step("dead-tuple growth from the hot-key burst")
	fmt.Printf("  n_live_tup=%d n_dead_tup=%d n_tup_upd=%d\n", stats.liveTup, stats.deadTup, stats.tupUpd)
	fmt.Printf("  -> %d updates against ONE row produced %d dead tuples, pending autovacuum\n",
		stats.tupUpd, stats.deadTup)
}

// ---- helpers ----

// timeSequential runs n single-threaded publishes, keyFn(i) chosen per call,
// returning total elapsed time in milliseconds.
func timeSequential(ctx context.Context, wpInstance *producer.ProducerInstance[common.Work], n int, keyFn func(i int) string) float64 {
	start := time.Now()
	for i := range n {
		opts := producer.ProduceOptions{}
		if key := keyFn(i); key != "" {
			compaction, err := producer.NewCompactionOptions(0)
			must(err)
			opts.MessageKey = key
			opts.Compaction = compaction
		}
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, opts)
		must(err)
	}
	return float64(time.Since(start).Microseconds()) / 1000.0
}

// timeConcurrent registers its own topic, fires goroutines*perGoroutine
// publishes across `goroutines` concurrent workers, and returns total
// elapsed time plus the topic name (caller destroys it once done reading it).
func timeConcurrent(ctx context.Context, ds *iDatastore.PostgresDatastore, label string, goroutines, perGoroutine int, keyFn func(g, i int) string) (float64, string) {
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	name := fmt.Sprintf("phase8c.compactionheadwritelab.%s.%d", label, time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), &topiccontroller.TopicConfig{PartitionSize: largePartitionSize})
	must(err)

	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)

	start := time.Now()
	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Go(func() {
			for i := range perGoroutine {
				compaction, err := producer.NewCompactionOptions(0)
				must(err)
				_, err = wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
					return common.NewWork(30, "admin@example.com")
				}, producer.ProduceOptions{MessageKey: keyFn(g, i), Compaction: compaction})
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

// compactionHeadTable resolves a topic's compaction_head_<id> table name from
// the catalog.
func compactionHeadTable(ctx context.Context, ds *iDatastore.PostgresDatastore, topicName string) string {
	var id int64
	must(ds.Pool.QueryRow(ctx, `SELECT id FROM topic_config WHERE name = $1;`, topicName).Scan(&id))
	return fmt.Sprintf("compaction_head_%d", id)
}

func dumpTableStats(ctx context.Context, ds *iDatastore.PostgresDatastore, table string) tableStats {
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
