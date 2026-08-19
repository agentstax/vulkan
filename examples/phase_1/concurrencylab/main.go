package main

// Buffered claim + N-processor dispatch concurrency proof.
//
// Two self-contained, self-verifying scenarios (registers its own topic,
// seeds its own backlog, destroys the topic on exit):
//
//  1. ORDERING -- one slow message and three fast ones claimed in the same
//     batch. Under a pool of 1, dispatch is strictly serial: the fast
//     messages can't even start until the slow one releases its only
//     permit, so every fast completion lands AFTER it. Under a pool of 4,
//     the fast messages dispatch to their own permits immediately and
//     finish while the slow one is still running -- every fast completion
//     lands BEFORE it. This is the whole reason buffered dispatch exists:
//     worker latency becomes max(slowest), not sum(all).
//
//  2. THROUGHPUT -- N fixed-cost messages drained once at pool=1 (serial,
//     wall time ~= N*cost) and once at pool=8 (parallel, wall time ~=
//     N*cost/8). Prints both RESULT lines and asserts the parallel run
//     beats the serial one by a wide margin.
//
// Both scenarios replay the SAME seeded backlog against independent
// consumer groups (independent cursors on the same topic), so nothing needs
// re-seeding between pool sizes.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	iCommon "github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const slowMs = 1000

func main() {
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{
		Pass: "example_password", MaxConns: 20,
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("phase14a.concurrencylab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)

	runOrdering(ctx, ds, wpInstance, tp.Name)
	runThroughput(ctx, ds, wpInstance, tp.Name)

	fmt.Println("\n✅ CONCURRENCY LAB PASSED")
	fmt.Println("   a slow message only blocks the rest of its batch when the pool can't run")
	fmt.Println("   around it (N=1) -- at N>1 the fast messages finish while it's still running,")
	fmt.Println("   and total throughput scales with pool size instead of message count alone.")
}

// ---- scenario 1: ordering ----

func runOrdering(ctx context.Context, ds *iDatastore.PostgresDatastore, wpInstance *producer.ProducerInstance[common.Work], topicName string) {
	step("ORDERING -- one slow message, three fast ones, same batch")
	seedSleep(ctx, wpInstance, []int{slowMs, 0, 0, 0})

	step("pool=1 -- dispatch is serial, fast messages can't start until the slow one releases its only permit")
	slowAt, fastAt := drain(ctx, ds, topicName, "phase14a.concurrencylab.n1", 1, 4)
	for i, at := range fastAt {
		if at < slowAt {
			die(fmt.Sprintf("pool=1: fast message %d completed at %s, before the slow message finished at %s -- dispatch should have been serial", i, at, slowAt))
		}
	}
	fmt.Printf("  ✓ all 3 fast completions landed after the slow one (%s)\n", slowAt)

	step("pool=4 -- fast messages dispatch to their own permits immediately, finish while the slow one is still running")
	slowAt, fastAt = drain(ctx, ds, topicName, "phase14a.concurrencylab.n4", 4, 4)
	for i, at := range fastAt {
		if at > slowAt {
			die(fmt.Sprintf("pool=4: fast message %d completed at %s, after the slow message finished at %s -- it should have run concurrently with it", i, at, slowAt))
		}
	}
	fmt.Printf("  ✓ all 3 fast completions landed before the slow one (%s) -- it didn't block them\n", slowAt)
}

// drain runs group over topicName's full backlog (assumed to fit in one
// claim -- batchLimit must cover it) at the given pool size, returning the
// slow message's completion offset from start and each fast message's.
func drain(ctx context.Context, ds *iDatastore.PostgresDatastore, topicName, group string, poolSize, batchLimit int) (time.Duration, []time.Duration) {
	wc, err := consumer.NewConsumer[common.Work](ds, &consumer.ConsumerConfig{
		DisableGracefulShutdown: true,
		BatchLimit:              batchLimit,
		QueueSize:               batchLimit + poolSize,
		MessageConcurrency:      poolSize,
		Message:                 &iCommon.MessageOptions{Timeout: 10 * time.Second},
		QueueMargin:             3 * time.Second,
		RecordMargin:            2 * time.Second,
	})
	must(err)
	wcInstance, err := wc.Register(ctx, group, topicName, topic.SchemaVersion(1), nil)
	must(err)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var slowAt time.Duration
	var fastAt []time.Duration
	var count atomic.Int64
	start := time.Now()

	must(wcInstance.Consume(runCtx, func(ctx context.Context, work *common.Work) error {
		if work.SleepMs > 0 {
			time.Sleep(time.Duration(work.SleepMs) * time.Millisecond)
		}
		at := time.Since(start)
		mu.Lock()
		if work.SleepMs > 0 {
			slowAt = at
		} else {
			fastAt = append(fastAt, at)
		}
		mu.Unlock()
		if count.Add(1) == int64(batchLimit) {
			// let this message's own commitRange land before shutdown races it --
			// harmless either way (closeOpenRanges settles it too) but noisy in the log.
			go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		}
		return nil
	}))
	return slowAt, fastAt
}

// ---- scenario 2: throughput ----

const (
	throughputCount = 40
	throughputCost  = 20 * time.Millisecond
	minSpeedup      = 3.0 // conservative vs pool=8's 8x theoretical ceiling -- avoids flaking on a loaded machine
)

func runThroughput(ctx context.Context, ds *iDatastore.PostgresDatastore, wpInstance *producer.ProducerInstance[common.Work], topicName string) {
	step("THROUGHPUT -- 40 fixed-cost messages, pool=1 (serial) vs pool=8 (parallel)")

	sleeps := make([]int, throughputCount)
	for i := range sleeps {
		sleeps[i] = int(throughputCost.Milliseconds())
	}
	seedSleep(ctx, wpInstance, sleeps)

	elapsed1 := drainTimed(ctx, ds, topicName, "phase14a.concurrencylab.tput1", 1, throughputCount)
	tput1 := float64(throughputCount) / elapsed1.Seconds()
	fmt.Printf("RESULT pool=1 processed=%d elapsed=%s throughput=%.1f/s\n", throughputCount, elapsed1, tput1)

	elapsed8 := drainTimed(ctx, ds, topicName, "phase14a.concurrencylab.tput8", 8, throughputCount)
	tput8 := float64(throughputCount) / elapsed8.Seconds()
	fmt.Printf("RESULT pool=8 processed=%d elapsed=%s throughput=%.1f/s\n", throughputCount, elapsed8, tput8)

	if speedup := tput8 / tput1; speedup < minSpeedup {
		die(fmt.Sprintf("pool=8 throughput only %.1fx pool=1's -- expected at least %.1fx", speedup, minSpeedup))
	} else {
		fmt.Printf("  ✓ pool=8 throughput is %.1fx pool=1's\n", tput8/tput1)
	}
}

func drainTimed(ctx context.Context, ds *iDatastore.PostgresDatastore, topicName, group string, poolSize, target int) time.Duration {
	wc, err := consumer.NewConsumer[common.Work](ds, &consumer.ConsumerConfig{
		DisableGracefulShutdown: true,
		BatchLimit:              target,
		QueueSize:               target + poolSize,
		MessageConcurrency:      poolSize,
		Message:                 &iCommon.MessageOptions{Timeout: 10 * time.Second},
		QueueMargin:             3 * time.Second,
		RecordMargin:            2 * time.Second,
	})
	must(err)
	wcInstance, err := wc.Register(ctx, group, topicName, topic.SchemaVersion(1), nil)
	must(err)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var count atomic.Int64
	var elapsed time.Duration
	start := time.Now()

	must(wcInstance.Consume(runCtx, func(ctx context.Context, work *common.Work) error {
		if work.SleepMs > 0 {
			time.Sleep(time.Duration(work.SleepMs) * time.Millisecond)
		}
		if count.Add(1) == int64(target) {
			elapsed = time.Since(start)
			// let this message's own commitRange land before shutdown races it --
			// harmless either way (closeOpenRanges settles it too) but noisy in the log.
			go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		}
		return nil
	}))
	return elapsed
}

// ---- helpers ----

func seedSleep(ctx context.Context, wpInstance *producer.ProducerInstance[common.Work], sleepMsList []int) {
	for _, ms := range sleepMsList {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			work, err := common.NewWork(30, "admin@example.com")
			if err != nil {
				return nil, err
			}
			work.SleepMs = ms
			return work, nil
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
