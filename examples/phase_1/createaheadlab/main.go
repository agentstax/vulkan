package main

// Create-ahead lab: the produce path creates the NEXT partition at the 80%
// trigger point, so the boundary insert never pays the failed-insert/DDL/retry
// heal. One scenario per append path -- per-call ProduceFunc, batched Produce,
// caller-owned-tx ProduceInTx -- each drives publishes across a partition
// boundary at lab width 100 and asserts:
//   - the next partition exists BEFORE any id needs it (polled after id 80)
//   - the "no partition covers" heal warn never fires
//   - ids stay contiguous (a heal burns the boundary id; create-ahead doesn't)
//   - creation never runs past the triggers' reach (no runaway chain)

import (
	"context"
	"fmt"
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/common/logging"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const partitionSize = int64(100)

// id 80 is partition 0's 80% trigger point; 105 crosses the boundary at 100
const triggerPublishes = int64(80)
const totalPublishes = int64(105)

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

	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	ds, err := iDatastore.NewPostgresDatastore(ctx, pool, nil)
	must(err)

	perCallScenario(ctx, ds)
	batchedScenario(ctx, ds)
	inTxScenario(ctx, ds)

	fmt.Println("\n✅ CREATE-AHEAD LAB PASSED")
	fmt.Println("   Every append path created the next partition at the 80% trigger point,")
	fmt.Println("   the boundary insert landed without a heal, and no id was burned.")
	return nil
}

// perCallScenario: ProduceFunc publishes one at a time -- the single-id
// trigger path (shouldTriggerWithId inside AppendMessage).
func perCallScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("per-call ProduceFunc: partition 1 exists before the boundary")
	tp, wpInstance, warns, cleanup := register(ctx, ds, "percall")
	defer cleanup()

	for range triggerPublishes {
		publish(ctx, wpInstance)
	}
	waitForPartition(ctx, ds, tp.Id, 1)

	for range totalPublishes - triggerPublishes {
		publish(ctx, wpInstance)
	}
	assertCreateAheadWon(ctx, ds, tp.Id, warns)
}

// batchedScenario: payload-only Produce calls ride the batcher -- the id-range
// trigger path (shouldTriggerWithRange inside AppendMessageBatch).
func batchedScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("batched Produce: a batch's id range fires the trigger before the boundary")
	tp, wpInstance, warns, cleanup := register(ctx, ds, "batched")
	defer cleanup()

	// 85 concurrent publishes cover id 80 inside some batch's range but stay
	// well under the boundary at 100
	publishConcurrent(ctx, wpInstance, 5, 17)
	waitForPartition(ctx, ds, tp.Id, 1)

	publishConcurrent(ctx, wpInstance, 4, 5) // 20 more cross the boundary
	assertCreateAheadWon(ctx, ds, tp.Id, warns)
}

// inTxScenario: the trigger id lands via ProduceInTx -- it fires pre-commit,
// and the create backs off until this tx's commit releases the parent lock.
func inTxScenario(ctx context.Context, ds *iDatastore.PostgresDatastore) {
	step("ProduceInTx: pre-commit trigger, create lands after the caller commits")
	tp, wpInstance, warns, cleanup := register(ctx, ds, "intx")
	defer cleanup()

	for range triggerPublishes - 1 {
		publish(ctx, wpInstance)
	}
	must(vulkan.InTransaction(ctx, ds, func(ctx context.Context, tx vulkan.Tx) error {
		_, err := wpInstance.ProduceInTx(ctx, tx, workFunc, nil) // id 80
		return err
	}))
	waitForPartition(ctx, ds, tp.Id, 1)

	for range totalPublishes - triggerPublishes {
		publish(ctx, wpInstance)
	}
	assertCreateAheadWon(ctx, ds, tp.Id, warns)
}

// ---- helpers ----

func register(ctx context.Context, ds *iDatastore.PostgresDatastore, scenario string) (*vulkan.TopicData, *vulkan.ProducerInstance[common.Work], *WarnCounter, func()) {
	warns, err := NewWarnCounter(logging.NewDefaultLogger(os.Stdout))
	must(err)
	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true, Logger: warns})
	must(err)

	topicName := fmt.Sprintf("createaheadlab.%s.%d", scenario, time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{PartitionSize: partitionSize})
	must(err)

	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)

	cleanup := func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}
	return tp, wpInstance, warns, cleanup
}

func workFunc(ctx context.Context, tx vulkan.Tx, _ string) (*common.Work, error) {
	return common.NewWork(30, "admin@example.com")
}

func publish(ctx context.Context, wpInstance *vulkan.ProducerInstance[common.Work]) {
	_, err := wpInstance.ProduceFunc(ctx, workFunc, nil)
	must(err)
}

func publishConcurrent(ctx context.Context, wpInstance *vulkan.ProducerInstance[common.Work], workers int, perWorker int) {
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range perWorker {
				work, err := common.NewWork(30, "admin@example.com")
				must(err)
				_, err = wpInstance.Produce(ctx, work, nil)
				must(err)
			}
		}()
	}
	wg.Wait()
}

// waitForPartition polls for message_log_<topicId>_<n> -- the creation is a
// detached goroutine, so "before the boundary" is proven by seeing the table
// while publishes are still below it.
func waitForPartition(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, n int64) {
	table := fmt.Sprintf("%s.%s", ds.Schema, topic.MessageLogPartitionTable(topicId, n))
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if regclassExists(ctx, ds, table) {
			fmt.Printf("  ✓ %s created ahead, head still below the boundary\n", table)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	die(fmt.Sprintf("%s was not created ahead within 10s", table))
}

// assertCreateAheadWon: no heal warn, no drop warn, ids contiguous (a heal
// burns the boundary id on its rolled-back insert), and only partition 1 was
// created ahead.
func assertCreateAheadWon(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, warns *WarnCounter) {
	assertInt("zero boundary-heal warns", warns.HealWarns.Load(), 0)
	assertInt("zero create-ahead drop warns", warns.DropWarns.Load(), 0)

	var count int64
	var maxId int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`
		SELECT count(*), COALESCE(max(id), 0) FROM %s.%s;
	`, ds.Schema, topic.MessageLogTable(topicId))).Scan(&count, &maxId))
	assertInt("every publish landed", count, totalPublishes)
	assertInt("ids contiguous -- no id burned at the boundary", maxId, totalPublishes)

	// a trigger creates the partition after the trigger id's own, so 105 ids
	// reach partition 1 only; partition 3 would be a runaway chain.
	if regclassExists(ctx, ds, fmt.Sprintf("%s.%s_3", ds.Schema, topic.MessageLogTable(topicId))) {
		die("partition 3 exists -- create-ahead ran away past the trigger's reach")
	}
	fmt.Println("  ✓ no runaway creation past the triggers' reach")
}

func regclassExists(ctx context.Context, ds *iDatastore.PostgresDatastore, table string) bool {
	var exists bool
	must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1) IS NOT NULL;`, table).Scan(&exists))
	return exists
}

// WarnCounter counts the two warns this lab must prove absent, delegating
// every record to the wrapped common.
type WarnCounter struct {
	HealWarns atomic.Int64
	DropWarns atomic.Int64

	inner logging.Logger
}

func NewWarnCounter(inner logging.Logger) (*WarnCounter, error) {
	if inner == nil {
		return nil, fmt.Errorf("inner logger must not be nil")
	}
	return &WarnCounter{inner: inner}, nil
}

func (w *WarnCounter) DebugContext(ctx context.Context, msg string, args ...any) {
	w.inner.DebugContext(ctx, msg, args...)
}
func (w *WarnCounter) InfoContext(ctx context.Context, msg string, args ...any) {
	w.inner.InfoContext(ctx, msg, args...)
}

// counted by attribute shape, never message text: both partition warns carry
// topic_id; only the create-ahead one carries an error value.
func (w *WarnCounter) WarnContext(ctx context.Context, msg string, args ...any) {
	if hasArgKey(args, "topic_id") {
		if hasArgKey(args, "error") {
			w.DropWarns.Add(1)
		} else {
			w.HealWarns.Add(1)
		}
	}
	w.inner.WarnContext(ctx, msg, args...)
}

func hasArgKey(args []any, key string) bool {
	for i := 0; i+1 < len(args); i += 2 {
		if name, ok := args[i].(string); ok && name == key {
			return true
		}
	}
	return false
}
func (w *WarnCounter) ErrorContext(ctx context.Context, msg string, args ...any) {
	w.inner.ErrorContext(ctx, msg, args...)
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
func assertInt(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
