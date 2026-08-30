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
	"github.com/agentstax/vulkan/pkg/admin"
	iCommon "github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
	concurrencyPtr := flag.Int("concurrency", 5, "worker pool size (concurrent consumerFuncs)")
	batchPtr := flag.Int("batch", 100, "claim batch limit (held constant across the sweep)")
	countPtr := flag.Int("count", 20000, "messages to process before stopping (should be <= seeded rows)")
	maxConnsPtr := flag.Int("maxconns", 25, "pgxpool max connections (must exceed concurrency+1)")
	groupPtr := flag.String("group", "phase3.bench", "consumer group name")
	topicPtr := flag.String("topic", "learning.v1", "topic to drain (must already have a seeded backlog, e.g. via `just produce`)")
	flag.Parse()

	conc := *concurrencyPtr
	batch := *batchPtr
	target := int64(*countPtr)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	// safety watchdog: never let a stalled run hang the sweep
	time.AfterFunc(180*time.Second, stop)

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{
		Pass:     "example_password",
		MaxConns: *maxConnsPtr,
	})
	if err != nil {
		return err
	}
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	if err != nil {
		return err
	}
	if err := mAdmin.RegisterSystem(ctx, nil); err != nil {
		return err
	}

	t, err := mAdmin.GetTopic(ctx, *topicPtr)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("topic %q is not registered -- `just produce` declares it\n", *topicPtr)
	}

	wc, err := consumer.NewConsumer[common.Work](ds, &consumer.ConsumerConfig{
		BatchLimit: batch,
		// buffer stays shallow but must be >= batch (validate) and big enough to keep the pool fed
		QueueSize:          batch + conc,
		MessageConcurrency: conc,
		Message:            &iCommon.MessageOptions{Timeout: 30 * time.Second, Retry: &iCommon.RetryPolicy{MaxRetries: 3}},
		ClaimPollRate:      500 * time.Millisecond,
		QueueMargin:        10 * time.Second,
		RecordMargin:       5 * time.Second,
	})
	if err != nil {
		return err
	}

	wcInstance, err := wc.Register(ctx, *groupPtr, t.Name, nil)
	if err != nil {
		return err
	}

	var counter atomic.Int64
	var firstNs, lastNs atomic.Int64
	start := time.Now()

	err = wcInstance.Consume(ctx, func(ctx context.Context, work *common.Work) error {
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
		return fmt.Errorf("consume error: %w", err)
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
	return nil
}
