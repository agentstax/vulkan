// Command bench drains a pre-seeded backlog of `ready` rows with a no-op
// consumerFunc and reports throughput (msgs/sec). It is the harness for the
// Phase 3 "Find the ceiling" lab: hold batch constant, sweep -concurrency,
// plot throughput vs worker count.
//
// It is deliberately silent (no per-message prints) so stdout is not the
// bottleneck, and it self-times from the first processed message to the
// target-th so DB-connect/startup cost is excluded from the rate.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
)

func main() {
	concurrencyPtr := flag.Int("concurrency", 5, "worker pool size (concurrent consumerFuncs)")
	batchPtr := flag.Int("batch", 100, "claim batch limit (held constant across the sweep)")
	countPtr := flag.Int("count", 20000, "messages to process before stopping (should be <= seeded rows)")
	maxConnsPtr := flag.Int("maxconns", 25, "pgxpool max connections (must exceed concurrency+1)")
	flag.Parse()

	conc := *concurrencyPtr
	batch := *batchPtr
	target := int64(*countPtr)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// safety watchdog: never let a stalled run hang the sweep
	time.AfterFunc(180*time.Second, stop)

	// buffer stays shallow but must be >= batch (validate) and big enough to keep the pool fed
	bufferSize := batch + conc
	pressureQueue, err := concurrency.NewPressureQueue[consumer.MessageRow](bufferSize)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	pool, err := concurrency.NewWorkerPoolLimiter(conc)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	pgParams := &consumer.PostgresConnectionParams{
		User:     "example_user",
		Pass:     "example_password",
		Host:     "localhost",
		Port:     5432,
		Database: "example_db",
		MaxConns: *maxConnsPtr,
	}
	datastore, err := consumer.NewPostgresDatastore[common.Work](ctx, pgParams)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	wc := consumer.NewWorkConsumer[common.Work](pressureQueue, pool, datastore)
	wc.WithBatchLimit(batch).
		WithMaxAttempts(3).
		WithClaimPollRate(500 * time.Millisecond).
		WithWorkTimeout(30 * time.Second).
		WithQueueTimeout(10 * time.Second).
		WithAckMargin(5 * time.Second).
		WithShutdownTimeout(40 * time.Second)

	var counter atomic.Int64
	var firstNs, lastNs atomic.Int64
	start := time.Now()

	err = wc.Consume(ctx, func(ctx context.Context, work *common.Work) error {
		n := counter.Add(1)
		if n == 1 {
			firstNs.Store(int64(time.Since(start)))
		}
		if n == target {
			lastNs.Store(int64(time.Since(start)))
			stop() // backlog target hit -> begin graceful shutdown
		}
		return nil // no-op: measures the queue machinery ceiling, not handler work
	})
	if err != nil {
		fmt.Println("consume error:", err)
		os.Exit(1)
	}

	processed := counter.Load()
	elapsed := time.Duration(lastNs.Load() - firstNs.Load())
	secs := elapsed.Seconds()
	var tput float64
	if secs > 0 {
		// first->last span, so DB connect + first claim are excluded
		tput = float64(processed-1) / secs
	}

	fmt.Printf("RESULT concurrency=%d batch=%d processed=%d seconds=%.3f throughput=%.1f\n",
		conc, batch, processed, secs, tput)
}
