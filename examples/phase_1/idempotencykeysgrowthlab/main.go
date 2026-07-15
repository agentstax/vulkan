package main

// idempotency_key growth lab: quantifies the sustained-throughput/storage
// axis of the claim-gate tradeoff -- distinct from a per-call fixed-cost/
// round-trip measurement (that's a separate question), this asks: does the
// claim gate's steady-state size become a real production concern, and can
// the janitor's sweep actually keep pace with it?
//
// Two scenarios:
//   - Accumulation & relative overhead: publish with no sweep running,
//     snapshot idempotency_key' size (delta against a pre-scenario
//     baseline, since it's a table shared across every topic) against this
//     topic's own message_log size at the same checkpoints -- puts "how
//     much extra storage" in concrete, relative terms instead of raw bytes.
//   - Sweep keep-up: sustained concurrent publishing WHILE the sweep runs
//     on the same cadence WorkConsumer's real Janitor loop uses (a ticker
//     firing SweepExpiredIdempotencyKeys at a fixed poll rate, batched) --
//     Little's Law says a keeping-up sweep should hold the table's
//     steady-state size near rate * ttl, not let it grow toward the full
//     count published; confirms that bound holds, and that a final pass
//     past ttl drains it to zero.
//
// Registers its own topics (destroyed on exit), self-seeded, self-verifying.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const largePartitionSize = int64(1_000_000) // never rolls -- partition churn isn't what's being measured

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
		MaxConns: 50, // headroom above the keep-up scenario's 30 concurrent publishers + sweeper
	})
	must(err)

	accumulationScenario(ctx, ds)
	sweepKeepUpScenario(ctx, ds)

	fmt.Println("\n✅ IDEMPOTENCY KEYS GROWTH LAB -- numbers gathered, see the idempotency_key")
	fmt.Println("   tradeoff discussion for what they mean and whether they change anything.")
}

// accumulationScenario: publish with the janitor never running, snapshot
// idempotency_key' size against message_log's own size at the same
// checkpoints -- isolates "how much extra storage does the claim gate cost"
// from the separate question (next scenario) of whether the sweep keeps up.
func accumulationScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("accumulation: idempotency_key size vs. message_log size, no sweep running")

	topicName := fmt.Sprintf("phase9.idempotencykeysgrowthlab.accum.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: largePartitionSize, IdempotencyKeyTTL: time.Hour})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)

	// idempotency_key is shared across every topic (unlike message_log) --
	// baseline it before publishing so cruft from other topics/labs in this
	// dev db doesn't leak into the delta being measured.
	baseline := tableByteSize(ctx, ds, "idempotency_key")

	checkpoints := []int{500, 2000, 5000}
	published := 0
	for _, target := range checkpoints {
		publishConcurrent(ctx, wp, target-published, 20)
		published = target

		idkDelta := tableByteSize(ctx, ds, "idempotency_key") - baseline
		// message_log_<id> is a partitioned parent with no storage of its own --
		// its data lives in message_log_<id>_0 (largePartitionSize never rolls).
		logSize := tableByteSize(ctx, ds, fmt.Sprintf("message_log_%d_0", tp.Id))
		idkRows := tableRowCountForTopic(ctx, ds, "idempotency_key", tp.Id)

		fmt.Printf("  %6d msgs: idempotency_key=+%-8s (%6d rows)  message_log=%8s  overhead=%.1f%%\n",
			published, humanBytes(idkDelta), idkRows, humanBytes(logSize), pctOf(idkDelta, logSize))
	}
}

// sweepKeepUpScenario: sustained concurrent publishing WHILE the sweep runs
// on the same cadence WorkConsumer's real Janitor loop uses (a ticker firing
// SweepExpiredIdempotencyKeys, batched) -- proves whether steady-state size
// stays bounded near rate * ttl (Little's Law) or the sweep falls behind and
// the table grows toward the full published count instead.
func sweepKeepUpScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("sweep keep-up: sustained publish load concurrent with the real janitor cadence")

	const ttl = 200 * time.Millisecond
	const sweepPollRate = 50 * time.Millisecond
	const sweepBatchSize = 500
	const duration = 3 * time.Second
	const publishers = 30

	topicName := fmt.Sprintf("phase9.idempotencykeysgrowthlab.keepup.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName, PartitionSize: largePartitionSize, IdempotencyKeyTTL: ttl})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)
	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)

	stop := make(chan struct{})
	var published atomic.Int64

	// N goroutines publishing flat-out for `duration`
	var wg sync.WaitGroup
	for range publishers {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
						return common.NewWork(30, "admin@example.com")
					}, producer.ProduceOptions{})
					must(err)
					published.Add(1)
				}
			}
		})
	}

	// mirrors WorkConsumer.Janitor's own ticker + sweep call, same shape
	var sweepWg sync.WaitGroup
	var peakRows atomic.Int64
	sweepWg.Go(func() {
		ticker := time.NewTicker(sweepPollRate)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				must(cd.SweepExpiredIdempotencyKeys(ctx, tp.Id, ttl, sweepBatchSize))
				if rows := tableRowCountForTopic(ctx, ds, "idempotency_key", tp.Id); rows > peakRows.Load() {
					peakRows.Store(rows)
				}
			}
		}
	})

	time.Sleep(duration)
	close(stop)
	wg.Wait()
	sweepWg.Wait()

	// one final sweep pass past ttl, so a fully-drained table proves the
	// sweep -- not just the test ending -- is what kept it bounded
	time.Sleep(ttl + 100*time.Millisecond)
	must(cd.SweepExpiredIdempotencyKeys(ctx, tp.Id, ttl, sweepBatchSize))
	finalRows := tableRowCountForTopic(ctx, ds, "idempotency_key", tp.Id)

	total := published.Load()
	peak := peakRows.Load()
	rate := float64(total) / duration.Seconds()
	// Little's Law: rows in the system ~= arrival rate * time each stays --
	// a sweep keeping pace should hold steady-state near rate*ttl, not let
	// it climb toward the full published count.
	littlesLawEstimate := rate * ttl.Seconds()

	fmt.Printf("  published %d messages over %v (%.0f msg/sec)\n", total, duration, rate)
	fmt.Printf("  Little's Law estimate (rate * ttl): ~%.0f rows steady-state if the sweep keeps pace\n", littlesLawEstimate)
	fmt.Printf("  peak idempotency_key rows observed during the run: %d (vs. %d total published)\n", peak, total)
	fmt.Printf("  final idempotency_key rows after one more pass past ttl: %d\n", finalRows)

	if finalRows != 0 {
		die(fmt.Sprintf("idempotency_key[topic %d] has %d rows after a full sweep pass past ttl, want 0", tp.Id, finalRows))
	}
	// generous slop factor -- poll interval, batch granularity, and goroutine
	// scheduling jitter all push peak above the exact Little's Law point
	// estimate, but a keeping-up sweep should stay an order of magnitude
	// below "grew unboundedly toward everything ever published."
	bound := int64(littlesLawEstimate*10) + sweepBatchSize
	if peak > bound {
		die(fmt.Sprintf("peak rows (%d) exceeded %dx the Little's Law estimate + one batch (%d) -- sweep fell behind publish load", peak, 10, bound))
	}
	if peak >= total {
		die(fmt.Sprintf("peak rows (%d) reached the full published count (%d) -- sweep never got ahead of publish load", peak, total))
	}
	fmt.Println("  ✓ steady-state size stayed bounded near the Little's Law estimate, and drained to 0 once ttl passed")
}

// ---- helpers ----

func publishConcurrent(ctx context.Context, wp *producer.WorkProducer[common.Work], n, goroutines int) {
	perGoroutine := n / goroutines
	remainder := n % goroutines

	var wg sync.WaitGroup
	for g := range goroutines {
		count := perGoroutine
		if g < remainder {
			count++
		}
		wg.Go(func() {
			for range count {
				_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
					return common.NewWork(30, "admin@example.com")
				}, producer.ProduceOptions{})
				must(err)
			}
		})
	}
	wg.Wait()
}

func tableByteSize(ctx context.Context, ds *coredatastore.PostgresDatastore, table string) int64 {
	var size int64
	must(ds.Pool.QueryRow(ctx, `SELECT pg_total_relation_size($1::regclass);`, table).Scan(&size))
	return size
}

func tableRowCountForTopic(ctx context.Context, ds *coredatastore.PostgresDatastore, table string, topicID int64) int64 {
	var count int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE topic_id = $1;`, table), topicID).Scan(&count))
	return count
}

func pctOf(part, whole int64) float64 {
	if whole == 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

func humanBytes(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(b)/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(b)/(1<<10))
	default:
		return fmt.Sprintf("%dB", b)
	}
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
