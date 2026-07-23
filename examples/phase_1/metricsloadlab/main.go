package main

// Phase 10 lab: run the real MessageConsumer.Consume loop under load with the
// debug readout on.
//
// Two scenarios:
//   - CATCH-UP: a pre-seeded burst, consumerFunc always succeeds. Measures
//     wall-clock time from Consume starting to `committed` reaching `head`,
//     once with a slow WaterlinePollRate and once with a fast one. Confirms
//     lowering WaterlinePollRate visibly narrows how fast committed catches
//     up -- the live, end-to-end counterpart to rolluplab's direct-datastore-
//     call staleness measurement (this drives it through the real Process/
//     RollWaterline goroutines and their tickers, not manual calls).
//   - LIVE READOUT: a burst with injected retryable failures. Watches the
//     debug readout while claimed/committed and exception counts move in
//     real time, and confirms every exception drains back out (none dead-
//     lettered) with no manual intervention -- DrainExceptions retrying
//     through the same live loop.

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const group = "phase10.metricsloadlab"

func main() {
	ctx := context.Background()

	slowElapsed := runCatchUpScenario(ctx, "slow WaterlinePollRate (2s)", 2*time.Second)
	fastElapsed := runCatchUpScenario(ctx, "fast WaterlinePollRate (100ms)", 100*time.Millisecond)

	fmt.Printf("\nslow poll catch-up: %s\nfast poll catch-up: %s\n", slowElapsed, fastElapsed)
	if fastElapsed >= slowElapsed {
		die(fmt.Sprintf("expected a lower WaterlinePollRate to catch up faster: fast=%s, slow=%s", fastElapsed, slowElapsed))
	}
	fmt.Printf("  ✓ lowering WaterlinePollRate cut catch-up time %.1fx (%s -> %s)\n", float64(slowElapsed)/float64(fastElapsed), slowElapsed, fastElapsed)

	runLiveReadoutScenario(ctx)

	fmt.Println("\n✅ METRICS LOAD LAB PASSED")
}

// ---- scenario 1: catch-up time vs. WaterlinePollRate ----

func runCatchUpScenario(ctx context.Context, label string, pollRate time.Duration) time.Duration {
	step(fmt.Sprintf("CATCH-UP SCENARIO: %s", label))

	consumerDS := newDS(ctx)
	defer consumerDS.Close()
	mAdmin, err := admin.NewMessageAdmin(consumerDS, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("%s.catchup.%d", group, time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topic.Config{})
	must(err)

	wp, err := producer.NewMessageProducer[common.Work](tp.Name, consumerDS, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))
	const rows = 100
	seed(ctx, wp, rows)

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](200)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(10)
	must(err)

	// BatchLimit >= rows -- everything claims in one Process tick, so the only
	// variable left between the two runs is how long RollWaterline's own
	// ticker takes to fire, isolating the thing this scenario measures.
	wc, err := consumer.NewMessageConsumer[common.Work](group, tp.Name, queue, pool, consumerDS, &consumer.MessageConsumerConfig{
		DisableGracefulShutdown: true,
		BatchLimit:              rows * 2,
		ClaimPollRate:           50 * time.Millisecond,
		WaterlinePollRate:       pollRate,
	})
	must(err)
	must(wc.Register(ctx))

	cctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		done <- wc.Consume(cctx, func(ctx context.Context, work *common.Work) error { return nil })
	}()

	elapsed := pollFor(ctx, wc, label, 20*time.Second, func(snap consumerQueueState) bool {
		return snap.Head > 0 && snap.Committed >= snap.Head
	})

	cancel()
	must(<-done)

	must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true}))

	fmt.Printf("  ✓ %s: committed caught up to head in %s\n", label, elapsed)
	return elapsed
}

// ---- scenario 2: live readout under injected failures ----

func runLiveReadoutScenario(ctx context.Context) {
	step("LIVE READOUT: consumer under load with injected retryable failures -- watch queue/exception counts move and drain")

	consumerDS := newDS(ctx)
	defer consumerDS.Close()
	mAdmin, err := admin.NewMessageAdmin(consumerDS, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("%s.readout.%d", group, time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, &topic.Config{})
	must(err)

	wp, err := producer.NewMessageProducer[common.Work](tp.Name, consumerDS, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))
	const rows = 60
	seed(ctx, wp, rows)

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](200)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(10)
	must(err)

	wc, err := consumer.NewMessageConsumer[common.Work](group, tp.Name, queue, pool, consumerDS, &consumer.MessageConsumerConfig{
		DisableGracefulShutdown: true,
		BatchLimit:              20,
		ClaimPollRate:           100 * time.Millisecond,
		WaterlinePollRate:       100 * time.Millisecond,
		ExceptionInitialBackoff: 200 * time.Millisecond,
		MaxAttempts:             5,
	})
	must(err)
	must(wc.Register(ctx))

	var mu sync.Mutex
	attempts := map[string]int{}
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		mu.Lock()
		attempts[work.Id]++
		n := attempts[work.Id]
		mu.Unlock()
		if n == 1 && rand.Float64() < 0.25 {
			return errors.New("artificial failure from -fail-rate")
		}
		return nil
	}

	cctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- wc.Consume(cctx, consumerFunc) }()

	sawException := false
	deadline := time.Now().Add(15 * time.Second)
	lastPrint := time.Time{}
	var final *consumerQueueState
	for {
		snap, err := wc.Metrics.Snapshot(ctx)
		must(err)
		if time.Since(lastPrint) >= 250*time.Millisecond {
			fmt.Println(snap.String())
			lastPrint = time.Now()
		}
		if snap.QueueState.ReadyExceptions+snap.QueueState.InflightExceptions > 0 {
			sawException = true
		}
		if snap.QueueState.Head > 0 && snap.QueueState.Committed >= snap.QueueState.Head &&
			snap.QueueState.ReadyExceptions == 0 && snap.QueueState.InflightExceptions == 0 {
			final = &consumerQueueState{Head: snap.QueueState.Head, Committed: snap.QueueState.Committed, DeadExceptions: snap.QueueState.DeadExceptions}
			break
		}
		if time.Now().After(deadline) {
			die("live readout scenario: gave up waiting for the backlog and all exceptions to drain after 15s")
		}
		time.Sleep(30 * time.Millisecond)
	}

	cancel()
	must(<-done)

	must(mAdmin.DestroyTopic(ctx, topicName, admin.DestroyOptions{Force: true}))

	if !sawException {
		die("expected at least one retryable exception to appear during the run -- fail-rate injection produced none")
	}
	assert("no dead-lettered messages -- every injected failure was retried and resolved", final.DeadExceptions, 0)
	fmt.Println("  ✓ exception counts moved during the run and fully drained back to zero -- claimed/committed gap closed with no manual intervention")
}

// ---- shared polling helper ----

// consumerQueueState is the subset of QueueStateSnapshot these scenarios poll on.
type consumerQueueState struct {
	Head, Committed, DeadExceptions int64
}

// pollFor polls wc's queue-state snapshot until until(snap) is true or
// timeout elapses, printing the debug readout at most every 250ms. Returns
// the time spent polling.
func pollFor(ctx context.Context, wc *consumer.MessageConsumer[common.Work], label string, timeout time.Duration, until func(consumerQueueState) bool) time.Duration {
	start := time.Now()
	deadline := start.Add(timeout)
	lastPrint := time.Time{}
	for {
		snap, err := wc.Metrics.QueueState.Snapshot(ctx)
		must(err)
		s := consumerQueueState{Head: snap.Head, Committed: snap.Committed, DeadExceptions: snap.DeadExceptions}
		if time.Since(lastPrint) >= 250*time.Millisecond {
			fmt.Printf("  [%s] head=%d claimed=%d committed=%d backlog=%d\n", label, snap.Head, snap.Claimed, snap.Committed, snap.Backlog)
			lastPrint = time.Now()
		}
		if until(s) {
			return time.Since(start)
		}
		if time.Now().After(deadline) {
			die(fmt.Sprintf("%s: gave up waiting after %s", label, timeout))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// ---- helpers ----

func newDS(ctx context.Context) *coredatastore.PostgresDatastore {
	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
		MaxConns: 20,
	})
	must(err)
	return ds
}

func seed(ctx context.Context, wp *producer.MessageProducer[common.Work], n int) {
	for range n {
		_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
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
func assert(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
