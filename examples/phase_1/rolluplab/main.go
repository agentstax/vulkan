package main

// Rollup lab: measures the numbers behind the lazy-vs-synchronous
// AdvanceWaterline decision (LEARNING_PLAN.md's "Resolve the lazy-vs-
// synchronous rollup"). Three scenarios:
//
//   - Staleness: how long after a range's Commit does `committed` actually
//     reflect it -- the lazy roller's own ticker (RollWaterline) vs. calling
//     AdvanceWaterline synchronously right after Commit. This is the gain.
//   - Fixed cost: sequential, uncontended -- the extra SELECT+UPDATE round
//     trip a synchronous call chains onto every Commit, isolated from any
//     lock contention.
//   - Concurrent contention: G goroutines committing against the SAME
//     (group, topic) cursor row -- Commit itself never touches that row
//     today (only lease + delivery), so a synchronous AdvanceWaterline
//     call is new contention on it, not a cost that already existed. Same
//     shape as latestkeyswritelab's hot-key scenario, applied to cursor.
//
// Registers its own topics (destroyed on exit), self-seeded.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	group            = "phase10.rolluplab"
	lease            = 30 * time.Second // long enough to never expire mid-lab
	maxRangeReclaims = 3
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
		MaxConns: 40, // headroom above the contention scenario's 20 concurrent goroutines
	})
	must(err)

	stalenessScenario(ctx, ds)
	fixedCostScenario(ctx, ds)
	contentionScenario(ctx, ds)

	fmt.Println("\n✅ ROLLUP LAB — numbers gathered, see LEARNING_PLAN.md's Phase 10")
	fmt.Println("   \"Resolve the lazy-vs-synchronous rollup\" bullet for the decision these drove.")
}

// ---- scenario 1: staleness ----

const (
	pollInterval  = 150 * time.Millisecond // stand-in for ClaimPollRate (default 5s -- see write-up)
	watchInterval = 2 * time.Millisecond   // fine-grained sampling so the detected-at time is trustworthy
	numRanges     = 30
	batchSize     = int64(5)
)

type rangeEvent struct {
	commitTime time.Time
	high       int64
}

type sample struct {
	t   time.Time
	val int64
}

func stalenessScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("staleness: time from Commit to `committed` reflecting it -- lazy ticker vs. synchronous")

	lazyEvents, lazySamples := runLazyStaleness(ctx, ds)
	lazyAvg, lazyMax := stalenessFromSamples(lazyEvents, lazySamples)

	syncStalenesses := runSyncStaleness(ctx, ds)
	syncAvg, syncMax := avgMax(syncStalenesses)

	fmt.Printf("  %-28s avg=%8.2fms  max=%8.2fms  (poll interval=%s)\n", "lazy (periodic roller)", lazyAvg, lazyMax, pollInterval)
	fmt.Printf("  %-28s avg=%8.2fms  max=%8.2fms\n", "synchronous (per-commit)", syncAvg, syncMax)
}

// runLazyStaleness commits numRanges ranges while a background ticker plays
// the role of RollWaterline, and a fast poller independently samples
// `committed` so staleness is measured from the outside, not self-reported.
func runLazyStaleness(ctx context.Context, ds *coredatastore.PostgresDatastore) ([]rangeEvent, []sample) {
	topicName := fmt.Sprintf("phase10.rolluplab.staleness.lazy.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)
	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)
	must(cd.UpsertCursor(ctx, tp.Id, group))
	seed(ctx, wp, int(int64(numRanges)*batchSize))

	watcherDone := make(chan struct{})
	samplesCh := make(chan []sample, 1)
	go func() {
		var samples []sample
		ticker := time.NewTicker(watchInterval)
		defer ticker.Stop()
		for {
			select {
			case <-watcherDone:
				samplesCh <- samples
				return
			case <-ticker.C:
				samples = append(samples, sample{t: time.Now(), val: committedCol(ctx, ds, group, tp.Id)})
			}
		}
	}()

	rollerDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-rollerDone:
				return
			case <-ticker.C:
				if _, err := cd.AdvanceWaterline(ctx, tp.Id, group); err != nil {
					fmt.Printf("  (roller tick error, ignored: %v)\n", err)
				}
			}
		}
	}()

	var events []rangeEvent
	for i := range numRanges {
		claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, int(batchSize), maxRangeReclaims, lease)
		must(err)
		if claim == nil {
			break
		}
		time.Sleep(jitter(i))
		must(cd.Commit(ctx, tp.Id, group, claim.Lease.Token, nil, nil, 5*time.Second))
		events = append(events, rangeEvent{commitTime: time.Now(), high: claim.Lease.High})
	}

	time.Sleep(pollInterval + 100*time.Millisecond) // let the final tick catch the last commit
	close(rollerDone)
	close(watcherDone)
	samples := <-samplesCh

	return events, samples
}

// runSyncStaleness commits numRanges ranges, calling AdvanceWaterline
// immediately after each Commit -- staleness is just that call's own latency.
func runSyncStaleness(ctx context.Context, ds *coredatastore.PostgresDatastore) []float64 {
	topicName := fmt.Sprintf("phase10.rolluplab.staleness.sync.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)
	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)
	must(cd.UpsertCursor(ctx, tp.Id, group))
	seed(ctx, wp, int(int64(numRanges)*batchSize))

	var stalenesses []float64
	for i := range numRanges {
		claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, int(batchSize), maxRangeReclaims, lease)
		must(err)
		if claim == nil {
			break
		}
		time.Sleep(jitter(i))
		must(cd.Commit(ctx, tp.Id, group, claim.Lease.Token, nil, nil, 5*time.Second))

		start := time.Now()
		_, err = cd.AdvanceWaterline(ctx, tp.Id, group)
		must(err)
		stalenesses = append(stalenesses, msSince(start))
	}
	return stalenesses
}

func stalenessFromSamples(events []rangeEvent, samples []sample) (avg, max float64) {
	var total float64
	var n int
	for _, e := range events {
		for _, s := range samples {
			if s.val >= e.high {
				d := s.t.Sub(e.commitTime).Seconds() * 1000
				if d < 0 {
					d = 0
				}
				total += d
				n++
				if d > max {
					max = d
				}
				break
			}
		}
	}
	if n == 0 {
		return 0, 0
	}
	return total / float64(n), max
}

// jitter stands in for real consumerFunc work -- small and deterministic so
// the lab's own runtime stays predictable.
func jitter(i int) time.Duration {
	return time.Duration(10+(i%5)*5) * time.Millisecond
}

// ---- scenario 2: fixed cost, uncontended ----

func fixedCostScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("fixed cost: sequential claim+commit, no contention -- commit-only vs. commit+synchronous-advance")

	const n = 200

	baselineMs := timeSequentialCommits(ctx, ds, "commitonly", n, false)
	syncMs := timeSequentialCommits(ctx, ds, "commitsync", n, true)

	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op\n", "commit only (lazy hot path)", baselineMs, baselineMs/n)
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op  (+%.1f%% vs. baseline)\n", "commit + synchronous advance", syncMs, syncMs/n, pctOver(syncMs, baselineMs))
}

func timeSequentialCommits(ctx context.Context, ds *coredatastore.PostgresDatastore, label string, n float64, syncAdvance bool) float64 {
	topicName := fmt.Sprintf("phase10.rolluplab.fixedcost.%s.%d", label, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)
	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)
	must(cd.UpsertCursor(ctx, tp.Id, group))
	seed(ctx, wp, int(n))

	start := time.Now()
	for range int(n) {
		claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, 1, maxRangeReclaims, lease)
		must(err)
		if claim == nil {
			break
		}
		must(cd.Commit(ctx, tp.Id, group, claim.Lease.Token, nil, nil, 5*time.Second))
		if syncAdvance {
			_, err := cd.AdvanceWaterline(ctx, tp.Id, group)
			must(err)
		}
	}
	return msSince(start)
}

// ---- scenario 3: concurrent contention ----

func contentionScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("concurrent contention: G goroutines committing against the SAME cursor row")

	const goroutines = 20
	const perGoroutine = 10
	total := goroutines * perGoroutine

	baseMs := timeConcurrentCommits(ctx, ds, "base", goroutines, perGoroutine, false)
	syncMs := timeConcurrentCommits(ctx, ds, "sync", goroutines, perGoroutine, true)

	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op (%d ops, %d goroutines)\n", "commit only (baseline)", baseMs, baseMs/float64(total), total, goroutines)
	fmt.Printf("  %-28s %10.3fms total  %8.4fms/op (%d ops, %d goroutines)\n", "commit + synchronous advance", syncMs, syncMs/float64(total), total, goroutines)
	fmt.Printf("  -> %.2fx slower with a synchronous rollup chained onto every commit\n", syncMs/baseMs)
}

func timeConcurrentCommits(ctx context.Context, ds *coredatastore.PostgresDatastore, label string, goroutines, perGoroutine int, syncAdvance bool) float64 {
	total := goroutines * perGoroutine
	topicName := fmt.Sprintf("phase10.rolluplab.contention.%s.%d", label, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)
	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewWorkProducer(tp, pd)
	must(cd.UpsertCursor(ctx, tp.Id, group))
	seed(ctx, wp, total)

	start := time.Now()
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for range perGoroutine {
				claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, 1, maxRangeReclaims, lease)
				must(err)
				if claim == nil {
					return
				}
				must(cd.Commit(ctx, tp.Id, group, claim.Lease.Token, nil, nil, 5*time.Second))
				if syncAdvance {
					_, err := cd.AdvanceWaterline(ctx, tp.Id, group)
					must(err)
				}
			}
		})
	}
	wg.Wait()
	return msSince(start)
}

// ---- helpers ----

func seed(ctx context.Context, wp *producer.WorkProducer[common.Work], n int) {
	for range n {
		_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
}

func committedCol(ctx context.Context, ds *coredatastore.PostgresDatastore, consumerGroup string, topicID int64) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, `SELECT committed FROM cursor WHERE consumer_group=$1 AND topic_id=$2`, consumerGroup, topicID).Scan(&v))
	return v
}

func msSince(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000.0
}

func avgMax(vals []float64) (avg, max float64) {
	if len(vals) == 0 {
		return 0, 0
	}
	var total float64
	for _, v := range vals {
		total += v
		if v > max {
			max = v
		}
	}
	return total / float64(len(vals)), max
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
