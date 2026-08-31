package main

// compaction-key deadlock lab: where reverse-ordered compaction_head lock
// cycles can and cannot happen.
//
// Registers its own topic (destroyed on exit), fully self-contained.
//
// Confirms, in order:
//   - default (batched) Produce cannot deadlock: every batch transaction
//     takes its compaction_head row locks in ascending key order (the
//     batcher's sort), so concurrent batchers hammering the same hot key
//     pool queue behind each other instead of cycling --
//     pg_stat_database.deadlocks stays flat across the run, every produce
//     lands, and each key's head converges to its true max id.
//   - two InTransaction callers producing the same two keys in reverse
//     order genuinely deadlock: Postgres kills exactly one (40P01) after
//     deadlock_timeout, the surfaced error classifies transient
//     (common.IsTransientPgError), and rerunning the victim's closure --
//     the caller-side retry the InTransaction docs require -- lands both
//     callers' messages and both keys' heads still converge.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"uuid"

	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/jackc/pgx/v5/pgconn"
)

type labMessage struct {
	Note string
}

func (labMessage) SchemaVersion() int { return 1 }

const (
	hotKeyCount          = 8
	producerCount        = 3
	goroutineCount       = 120
	producesPerGoroutine = 25
)

var (
	ds        *iDatastore.PostgresDatastore
	client    *vulkan.Client
	topicName string
	topicId   int64
	// wpInstance is the shared instance scenario 2 seeds and produces through.
	wpInstance *vulkan.ProducerInstance[labMessage]
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

	ds, err = iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	client, err = vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	topicName = fmt.Sprintf("compactiondeadlocklab.%d", time.Now().UnixNano())
	registered, err := client.RegisterTopic(ctx, topicName, nil)
	must(err)
	topicId = registered.Id
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	wpInstance, err = client.RegisterProducer[labMessage](ctx, topicName, nil)
	must(err)

	batcherAbsenceScenario(ctx)
	produceInTxDeadlockScenario(ctx)

	fmt.Println("\n✅ COMPACTION DEADLOCK LAB PASSED")
	fmt.Println("   batched produce over a hot key pool raised zero deadlocks (ascending")
	fmt.Println("   key order holds across concurrent batchers); reverse-ordered")
	fmt.Println("   InTransaction callers raised exactly one 40P01, classified transient,")
	fmt.Println("   and a caller-side rerun landed both -- heads converged either way.")
	return nil
}

// batcherAbsenceScenario hammers default Produce from several producer
// instances over one small key pool, every goroutine walking the pool from a
// different starting offset -- maximal reverse-order pressure at enqueue,
// which the batcher's ascending sort must neutralize.
func batcherAbsenceScenario(ctx context.Context) {
	step("default produce: concurrent batchers over a hot key pool never deadlock")
	deadlocksBefore := deadlockCount(ctx)

	instances := make([]*vulkan.ProducerInstance[labMessage], producerCount)
	for i := range instances {
		instance, err := client.RegisterProducer[labMessage](ctx, topicName, nil)
		must(err)
		instances[i] = instance
	}

	keys := make([]string, hotKeyCount)
	for i := range keys {
		keys[i] = fmt.Sprintf("hot-%d", i)
	}

	var errOnce sync.Once
	var firstErr error
	record := func(err error) { errOnce.Do(func() { firstErr = err }) }

	started := time.Now()
	var wg sync.WaitGroup
	for i := range goroutineCount {
		wg.Add(1)
		go func(instance *vulkan.ProducerInstance[labMessage], offset int) {
			defer wg.Done()
			for produced := range producesPerGoroutine {
				key := keys[(offset+produced)%len(keys)]
				compaction, err := vulkan.NewCompactionOptions(0)
				if err != nil {
					record(err)
					return
				}
				if _, err := instance.Produce(ctx, &labMessage{Note: key}, &vulkan.ProduceOptions{MessageKey: key, Compaction: compaction}); err != nil {
					record(err)
					return
				}
			}
		}(instances[i%producerCount], i)
	}
	wg.Wait()
	must(firstErr)
	elapsed := time.Since(started)

	total := goroutineCount * producesPerGoroutine
	assertInt64("every produce landed as a row", messageCount(ctx), int64(total))
	for _, key := range keys {
		assertHeadIsMaxId(ctx, key)
	}

	// pg_stat flushes per backend within ~1s of going idle -- settle, then read
	time.Sleep(2 * time.Second)
	assertInt64("deadlocks raised during the run", deadlockCount(ctx)-deadlocksBefore, 0)
	fmt.Printf("  ✓ %d produces, %d goroutines, %d hot keys, zero deadlocks (%.0f msgs/s)\n",
		total, goroutineCount, hotKeyCount, float64(total)/elapsed.Seconds())
}

// produceInTxDeadlockScenario is TEST.md's RETRY-40P01 scenario made
// runnable: caller A produces tx-a then tx-b, caller B produces tx-b then
// tx-a, a barrier between the first and second produces guaranteeing both
// hold their first head row lock before either wants the second.
func produceInTxDeadlockScenario(ctx context.Context) {
	step("reverse-ordered ProduceInTx: one 40P01 victim, caller-side rerun lands both")

	// seed both head rows so the second produces contend on existing rows
	for _, key := range []string{"tx-a", "tx-b"} {
		compaction, err := vulkan.NewCompactionOptions(0)
		must(err)
		_, err = wpInstance.Produce(ctx, &labMessage{Note: "seed"}, &vulkan.ProduceOptions{MessageKey: key, Compaction: compaction})
		must(err)
	}
	deadlocksBefore := deadlockCount(ctx)

	aHoldsFirst := make(chan struct{})
	bHoldsFirst := make(chan struct{})
	var aOnce, bOnce sync.Once
	results := make(chan callerResult, 2)
	go func() {
		results <- runCallerWithRetry(ctx, "tx-a", "tx-b", &aOnce, aHoldsFirst, bHoldsFirst)
	}()
	go func() {
		results <- runCallerWithRetry(ctx, "tx-b", "tx-a", &bOnce, bHoldsFirst, aHoldsFirst)
	}()

	deadlocksSeen := 0
	var victimWait time.Duration
	for range 2 {
		result := <-results
		must(result.err)
		deadlocksSeen += result.deadlocks
		if result.victimWait > victimWait {
			victimWait = result.victimWait
		}
	}
	assertInt64("40P01s surfaced to the callers", int64(deadlocksSeen), 1)

	waitDeadlockCount(ctx, deadlocksBefore+1)
	for _, key := range []string{"tx-a", "tx-b"} {
		// one seed + one row per caller
		assertInt64(fmt.Sprintf("rows under %q", key), keyMessageCount(ctx, key), 3)
		assertHeadIsMaxId(ctx, key)
	}
	fmt.Printf("  ✓ exactly one victim, killed after %v (deadlock_timeout), rerun landed both\n",
		victimWait.Round(10*time.Millisecond))
}

type callerResult struct {
	deadlocks  int
	victimWait time.Duration
	err        error
}

// runCallerWithRetry is one InTransaction caller plus the retry loop the
// InTransaction docs leave to the caller: a 40P01 must classify transient
// and the whole closure reruns. The barrier only gates the first attempt --
// once both channels closed, a rerun sails through against the already
// committed survivor.
func runCallerWithRetry(ctx context.Context, firstKey string, secondKey string, holdOnce *sync.Once, holdsFirst chan struct{}, peerHoldsFirst chan struct{}) callerResult {
	// fixed per-produce keys make the rerun dedup-safe, the documented pattern
	firstIdempotencyKey := uuid.NewV7().String()
	secondIdempotencyKey := uuid.NewV7().String()

	result := callerResult{}
	for range 5 {
		started := time.Now()
		err := vulkan.InTransaction(ctx, ds, func(ctx context.Context, tx vulkan.Tx) error {
			if err := produceKeyInTx(ctx, tx, firstKey, firstIdempotencyKey); err != nil {
				return err
			}

			holdOnce.Do(func() { close(holdsFirst) })
			<-peerHoldsFirst
			return produceKeyInTx(ctx, tx, secondKey, secondIdempotencyKey)
		})
		if err == nil {
			return result
		}

		pgErr, ok := errors.AsType[*pgconn.PgError](err)
		if !ok || pgErr.Code != "40P01" {
			result.err = fmt.Errorf("want only 40P01 surfacing: %w", err)
			return result
		}
		if !common.IsTransientPgError(err) {
			result.err = fmt.Errorf("40P01 must classify transient: %w", err)
			return result
		}
		result.deadlocks++
		result.victimWait = time.Since(started)
	}
	result.err = errors.New("caller never committed within 5 attempts")
	return result
}

func produceKeyInTx(ctx context.Context, tx vulkan.Tx, key string, idempotencyKey string) error {
	compaction, err := vulkan.NewCompactionOptions(0)
	if err != nil {
		return err
	}
	_, err = wpInstance.ProduceInTx(ctx, tx, func(ctx context.Context, tx vulkan.Tx, _ string) (*labMessage, error) {
		return &labMessage{Note: key}, nil
	}, &vulkan.ProduceOptions{MessageKey: key, Compaction: compaction, IdempotencyKey: idempotencyKey})
	return err
}

func deadlockCount(ctx context.Context) int64 {
	var count int64
	must(ds.Pool.QueryRow(ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname = current_database();`).Scan(&count))
	return count
}

// waitDeadlockCount polls until the database-wide counter reaches want --
// pg_stat lags each backend's flush, so a plain read races it.
func waitDeadlockCount(ctx context.Context, want int64) {
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := deadlockCount(ctx)
		if got == want {
			return
		}
		if got > want {
			die(fmt.Sprintf("deadlock counter overshot: got %d, want %d", got, want))
		}
		if time.Now().After(deadline) {
			die(fmt.Sprintf("deadlock counter never reached %d, still %d", want, got))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func messageCount(ctx context.Context) int64 {
	var count int64
	must(ds.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM message_log_%d WHERE message_key LIKE 'hot-%%';`, topicId)).Scan(&count))
	return count
}

func keyMessageCount(ctx context.Context, key string) int64 {
	var count int64
	must(ds.Pool.QueryRow(ctx,
		fmt.Sprintf(`SELECT COUNT(*) FROM message_log_%d WHERE message_key = $1;`, topicId), key).Scan(&count))
	return count
}

// assertHeadIsMaxId confirms a key's compaction_head row points at the key's
// highest message id -- all lab produces are rank 0, so arrival order (the id
// tiebreak) must decide every winner no matter how commits interleaved.
func assertHeadIsMaxId(ctx context.Context, key string) {
	var headId int64
	var maxId int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT
			h.head_id,
			(SELECT MAX(id) FROM message_log_%d WHERE message_key = $1)
		FROM compaction_head_%d h
		WHERE h.compaction_key = $1;`, topicId, topicId), key).Scan(&headId, &maxId))
	if headId != maxId {
		die(fmt.Sprintf("head for %q must converge to the max id: got %d, want %d", key, headId, maxId))
	}
}

func assertInt64(name string, got int64, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", name, got, want))
	}
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
