package main

// Producer batching acceptance lab: the permanent home for what the
// throwaway in-package batcher harness verified, driven entirely through
// the public API. Five scenarios:
//
//   - batchedExactlyOnceScenario: N concurrent callers through payload-only
//     Produce all land exactly once (messages AND claim rows), xmin grouping
//     proves calls actually shared transactions, and a caller-keyed Produce
//     routes to the per-call path and dedups across its own retries.
//   - faultIsolationScenario: one server-side-poisoned payload (jsonb
//     rejects \u0000) and one client-side-unencodable payload each fail ONLY
//     their own caller -- everyone sharing their batches still lands.
//   - hotCompactionKeysScenario: hot compaction keys inside concurrent
//     batches -- the per-batch key sort keeps latest_key lock order global,
//     so nothing deadlocks and every latest_key row ends at its key's max id.
//   - partitionHealScenario: a burst that outruns the janitor's create-ahead
//     (no janitor running at all here) self-heals missing partitions,
//     sequentially and under concurrency.
//   - throughputScenario: the numbers -- batched Produce vs per-call
//     ProduceFunc at equal concurrency, plus a saturated batched arm
//     (callers >> batch cap). The retired SkipIdempotency floor's numbers
//     live in bench/idempotency/RESULTS.md's follow-up section -- batched
//     lapped it ~10x saturated, which is why it could be removed.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const largePartitionSize = int64(1_000_000) // never rolls -- partition churn is its own scenario

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
		MaxConns: 60, // headroom above the per-call arms' 50 concurrent publishers -- batched callers wait on a channel, not a connection, so even the 800-caller saturated arm needs no more
	})
	must(err)
	defer ds.Close()

	batchedExactlyOnceScenario(ctx, ds)
	faultIsolationScenario(ctx, ds)
	hotCompactionKeysScenario(ctx, ds)
	partitionHealScenario(ctx, ds)
	throughputScenario(ctx, ds)

	fmt.Println("\n✅ PRODUCER BATCH LAB PASSED")
	fmt.Println("   Concurrent payload-only Produce calls share transactions and land exactly")
	fmt.Println("   once, poisoned payloads fail only their own caller, hot compaction keys")
	fmt.Println("   never deadlock, bursts self-heal missing partitions, and the throughput")
	fmt.Println("   numbers above show what batching buys over the per-call paths.")
}

// batchedExactlyOnceScenario: 50 goroutines x 20 payload-only Produce calls
// -- every one must land exactly once (message + claim row), and xmin
// grouping must show multi-row transactions, or "batching" never happened.
func batchedExactlyOnceScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("batched exactly-once: concurrent Produce calls share txns, land once each")

	const producers, msgs = 50, 20
	tp, cleanup := registerTopic(ctx, ds, "exactlyonce", largePartitionSize)
	defer cleanup()

	wp, err := producer.NewMessageProducer[common.Work](tp, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	produceConcurrently(producers, msgs, func(p, s int) error {
		work, err := common.NewWork(30, "admin@example.com")
		if err != nil {
			return err
		}
		_, err = wp.Produce(ctx, work, producer.ProduceOptions{})
		return err
	})

	total := producers * msgs
	assertCount(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), total, fmt.Sprintf("%d concurrent batched publishes all landed", total))
	assertCount(ctx, ds, fmt.Sprintf("idempotency_key_%d", tp.Id), total, "every batched publish wrote its own claim row")

	// rows committed by one txn share xmin -- any multi-row xmin proves grouping
	var sharedTxns, largestBatch int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*), COALESCE(max(c), 0) FROM (
			SELECT count(*) AS c FROM message_log_%d GROUP BY xmin HAVING count(*) > 1
		) shared;
	`, tp.Id)).Scan(&sharedTxns, &largestBatch))
	if sharedTxns == 0 {
		die("no shared transactions observed -- 50 concurrent callers never grouped into a batch")
	}
	fmt.Printf("  ✓ calls genuinely shared transactions (%d shared txns, largest batch %d)\n", sharedTxns, largestBatch)

	// caller-keyed calls leave the batch: same key twice = one message
	key := uuid.Must(uuid.NewV7())
	for range 2 {
		work, err := common.NewWork(31, "keyed@example.com")
		must(err)
		_, err = wp.Produce(ctx, work, producer.ProduceOptions{IdempotencyKey: key})
		must(err)
	}
	assertCount(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), total+1, "a caller-keyed Produce routed per-call and deduped its retry")
}

// faultIsolationScenario: 20 concurrent produces where payload 7 is rejected
// server-side (jsonb refuses \u0000 -- poisons every rerun, evicted by
// statement index) and payload 13 fails client-side before anything is sent
// (batch splits to singles) -- each error reaches ONLY its own caller.
func faultIsolationScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("fault isolation: a bad payload fails its caller, never its batchmates")

	const total = 20
	const poisonSeq, brokenSeq = 7, 13
	tp, cleanup := registerTopic(ctx, ds, "faults", largePartitionSize)
	defer cleanup()

	wp, err := producer.NewMessageProducer[json.RawMessage](tp, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	errs := make([]error, total)
	var wg sync.WaitGroup
	for s := range total {
		wg.Go(func() {
			payload := json.RawMessage(fmt.Sprintf(`{"seq": %d}`, s))
			switch s {
			case poisonSeq:
				payload = json.RawMessage(`{"seq": "\u0000"}`)
			case brokenSeq:
				payload = json.RawMessage(`{"broken`)
			}
			_, errs[s] = wp.Produce(ctx, &payload, producer.ProduceOptions{})
		})
	}
	wg.Wait()

	for s, err := range errs {
		switch s {
		case poisonSeq, brokenSeq:
			if err == nil {
				die(fmt.Sprintf("bad payload %d produced without error", s))
			}
		default:
			if err != nil {
				die(fmt.Sprintf("good payload %d caught its batchmate's error: %v", s, err))
			}
		}
	}
	fmt.Println("  ✓ both bad payloads errored, all good payloads did not")
	assertCount(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), total-2, "every good payload landed despite sharing batches with the bad ones")
}

// hotCompactionKeysScenario: 20 goroutines x 20 keyed produces across only 3
// hot keys, with a tiny batch cap so multiple workers commit concurrently --
// real cross-batch latest_key contention. A deadlock would surface as an
// evicted operation's error; zero errors + every latest_key row at its key's
// max id is the pass.
func hotCompactionKeysScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("hot compaction keys: concurrent batches contend on latest_key without deadlock")

	const producers, msgs, keys = 20, 20, 3
	tp, cleanup := registerTopic(ctx, ds, "hotkeys", largePartitionSize)
	defer cleanup()

	// tiny cap -> backlog pressure -> concurrent workers -> real lock contention
	wp, err := producer.NewMessageProducer[common.Work](tp, ds, &producer.MessageProducerConfig{BatchMaxSize: 5, DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	produceConcurrently(producers, msgs, func(p, s int) error {
		work, err := common.NewWork(30, "admin@example.com")
		if err != nil {
			return err
		}
		_, err = wp.Produce(ctx, work, producer.ProduceOptions{CompactionKey: fmt.Sprintf("hot:%d", (p+s)%keys)})
		return err
	})

	total := producers * msgs
	assertCount(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), total, fmt.Sprintf("%d hot-keyed publishes all landed, none deadlocked", total))

	var stale int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*) FROM latest_key lk
		JOIN (
			SELECT compaction_key, max(id) AS max_id FROM message_log_%d GROUP BY compaction_key
		) m ON m.compaction_key = lk.compaction_key
		WHERE lk.topic_id = $1 AND lk.latest_id <> m.max_id;
	`, tp.Id), tp.Id).Scan(&stale))
	if stale != 0 {
		die(fmt.Sprintf("%d latest_key rows not pointing at their key's max id", stale))
	}
	fmt.Printf("  ✓ every latest_key row points at its key's max id across %d hot keys\n", keys)
}

// partitionHealScenario: PartitionSize 10 and no janitor, so produces past
// partition 0 MUST self-heal -- first sequentially (head advances one heal at
// a time), then as a concurrent burst.
func partitionHealScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("partition heal: a burst past the create-ahead self-heals, no janitor running")

	tp, cleanup := registerTopic(ctx, ds, "heal", 10)
	defer cleanup()

	// cap <= PartitionSize so one heal covers a whole batch
	wp, err := producer.NewMessageProducer[common.Work](tp, ds, &producer.MessageProducerConfig{BatchMaxSize: 5, DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	for range 15 {
		work, err := common.NewWork(30, "admin@example.com")
		must(err)
		_, err = wp.Produce(ctx, work, producer.ProduceOptions{})
		must(err)
	}
	produceConcurrently(8, 5, func(p, s int) error {
		work, err := common.NewWork(30, "admin@example.com")
		if err != nil {
			return err
		}
		_, err = wp.Produce(ctx, work, producer.ProduceOptions{})
		return err
	})

	assertCount(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), 55, "all 55 publishes landed across self-healed partitions")
}

// throughputScenario: the same workload down both paths -- how much the
// shared-transaction fsync amortization buys over per-call commits, at equal
// concurrency and then saturated.
func throughputScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("throughput: batched Produce vs per-call ProduceFunc, then saturated")

	const producers, msgs = 50, 400 // ~2s per arm -- sub-second runs are all warmup noise
	total := producers * msgs

	batched := timeArm(ctx, ds, "batched", producers, msgs, func(wp *producer.MessageProducer[common.Work], work *common.Work) error {
		_, err := wp.Produce(ctx, work, producer.ProduceOptions{})
		return err
	})
	perCall := timeArm(ctx, ds, "percall", producers, msgs, func(wp *producer.MessageProducer[common.Work], work *common.Work) error {
		_, err := wp.ProduceFunc(ctx, func(context.Context, producer.Tx, uuid.UUID) (*common.Work, error) { return work, nil }, producer.ProduceOptions{})
		return err
	})

	// the three arms share ONE caller count so the ratio is fair -- but N
	// callers blocked ~one commit each also CAPS arrival (Little's law), so
	// none of them is the batcher's ceiling. Saturate it: enough callers that
	// batches ride at BatchMaxSize, which only the batched path can absorb
	// (a per-call arm would need a pool connection per caller).
	const satProducers, satMsgs = 800, 50
	saturated := timeArm(ctx, ds, "saturated", satProducers, satMsgs, func(wp *producer.MessageProducer[common.Work], work *common.Work) error {
		_, err := wp.Produce(ctx, work, producer.ProduceOptions{})
		return err
	})
	satRate := float64(satProducers*satMsgs) / saturated.Seconds()

	rate := func(elapsed time.Duration) float64 { return float64(total) / elapsed.Seconds() }
	fmt.Printf("  %-42s %8s %12s\n", fmt.Sprintf("arm (%d goroutines x %d msgs)", producers, msgs), "elapsed", "msgs/s")
	fmt.Printf("  %-42s %7.2fs %12.0f\n", "batched Produce", batched.Seconds(), rate(batched))
	fmt.Printf("  %-42s %7.2fs %12.0f\n", "per-call ProduceFunc", perCall.Seconds(), rate(perCall))
	fmt.Printf("  %-42s %7.2fs %12.0f\n", fmt.Sprintf("batched Produce, saturated (%d callers)", satProducers), saturated.Seconds(), satRate)
	fmt.Printf("  -> batched is %.1fx per-call at equal concurrency, %.1fx once callers\n", rate(batched)/rate(perCall), satRate/rate(perCall))
	fmt.Printf("     saturate the batch cap\n")
}

// timeArm registers its own topic, warms the pool untimed, then times the
// full concurrent run -- asserting afterward that every publish landed
// exactly once (throughput that loses messages doesn't count).
func timeArm(ctx context.Context, ds *coredatastore.PostgresDatastore, label string, producers, msgs int, produce func(wp *producer.MessageProducer[common.Work], work *common.Work) error) time.Duration {
	tp, cleanup := registerTopic(ctx, ds, "throughput."+label, largePartitionSize)
	defer cleanup()

	wp, err := producer.NewMessageProducer[common.Work](tp, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	// warm pool connections so the first arm doesn't pay the dial cost
	warm := producers
	produceConcurrently(warm, 1, func(p, s int) error {
		work, err := common.NewWork(30, "warmup@example.com")
		if err != nil {
			return err
		}
		return produce(wp, work)
	})

	start := time.Now()
	produceConcurrently(producers, msgs, func(p, s int) error {
		work, err := common.NewWork(30, "admin@example.com")
		if err != nil {
			return err
		}
		return produce(wp, work)
	})
	elapsed := time.Since(start)

	assertCount(ctx, ds, fmt.Sprintf("message_log_%d", tp.Id), warm+producers*msgs, label+" arm landed every publish exactly once")
	return elapsed
}

// ---- helpers ----

// registerTopic registers a lab-unique topic and returns it with its cleanup.
func registerTopic(ctx context.Context, ds *coredatastore.PostgresDatastore, label string, partitionSize int64) (*topic.Topic, func()) {
	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	name := fmt.Sprintf("producerbatchlab.%s.%d", label, time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, name, &topic.Config{PartitionSize: partitionSize})
	must(err)
	return tp, func() { must(mAdmin.DestroyTopic(ctx, name, admin.DestroyOptions{Force: true})) }
}

// produceConcurrently fans producers goroutines x msgs calls each and dies on
// the first error any of them hit.
func produceConcurrently(producers, msgs int, produce func(p, s int) error) {
	errCh := make(chan error, producers*msgs)
	var wg sync.WaitGroup
	for p := range producers {
		wg.Go(func() {
			for s := range msgs {
				if err := produce(p, s); err != nil {
					errCh <- fmt.Errorf("producer %d seq %d: %w", p, s, err)
				}
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		die(err.Error())
	}
}

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
	fmt.Printf("\n❌ LAB FAILED: %s\n", strings.TrimSpace(msg))
	os.Exit(1)
}
