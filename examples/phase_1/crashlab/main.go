// Command crashlab is the Phase 3.5 "crash-after-async-commit" lab harness.
//
// It drains a pre-seeded backlog and appends every processed message id to a log
// file — the *application's durable record of what it believed it processed*.
// An external orchestration script kills Postgres mid-run with
// synchronous_commit=off, restarts it, and runs this harness again to drain the
// reclaimed backlog. Comparing the app log to the recovered DB state proves:
//   - no message is lost (every seeded id appears >=1 time)  -> at-least-once holds
//   - the acks lost in the crash are reprocessed (some ids appear 2+ times)
//
// Run with synchronous_commit=on as a control: the acks are durable, so the
// reprocessed set collapses to just the in-flight-at-crash messages.
//
// The log id is the payload "id" field, which the seed sets equal to the
// topic's message_log row id (TRUNCATE ... RESTART IDENTITY), so app log and DB align.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
)

func main() {
	concurrencyPtr := flag.Int("concurrency", 8, "worker pool size")
	countPtr := flag.Int("count", 0, "messages to process before a clean stop")
	logPtr := flag.String("log", "/tmp/crashlab_processed.log", "append each processed id here (the app's record)")
	maxConnsPtr := flag.Int("maxconns", 20, "pgxpool max connections")
	groupPtr := flag.String("group", "phase3_5.crashlab", "consumer group name")
	topicPtr := flag.String("topic", "learning.v1", "topic to drain (must already have a seeded backlog, e.g. via `just produce`)")
	flag.Parse()

	conc := *concurrencyPtr
	target := int64(*countPtr)
	if target < 1 {
		fmt.Println("-count must be >= 1")
		os.Exit(1)
	}

	logFile, err := os.OpenFile(*logPtr, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer logFile.Close()
	w := bufio.NewWriter(logFile)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	time.AfterFunc(180*time.Second, stop) // watchdog

	queue, err := concurrency.NewPressureQueue[consumer.MessageRow](100 + conc)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	pool, err := concurrency.NewWorkerPoolLimiter(conc)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password", Host: "localhost", Port: 5432, Database: "example_db", MaxConns: *maxConnsPtr,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
	defer ds.Close()

	t, err := topic.Register(ctx, ds, &topic.Config{Name: *topicPtr})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	// Short lease (= WorkTimeout+QueueMargin+AckMargin = 4s) so in-flight rows
	// reclaim quickly after the crash. High MaxAttempts so reprocessing never
	// dead-letters — we want pure at-least-once redelivery, not the DLQ path.
	wc, err := consumer.NewMessageConsumer[common.Work](*groupPtr, t, queue, pool, ds, &consumer.MessageConsumerConfig{
		BatchLimit:    100,
		MaxAttempts:   100,
		ClaimPollRate: 200 * time.Millisecond,
		WorkTimeout:   2 * time.Second,
		QueueMargin:   1 * time.Second,
		AckMargin:     1 * time.Second,
	})
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if err := wc.Register(ctx); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	var mu sync.Mutex
	var counter atomic.Int64

	err = wc.Consume(ctx, func(ctx context.Context, work *common.Work) error {
		if work.SleepMs > 0 {
			time.Sleep(time.Duration(work.SleepMs) * time.Millisecond) // throttle so we can crash mid-run
		}
		mu.Lock()
		w.WriteString(work.Id + "\n")
		w.Flush() // keep the app log current so a kill -9 of THIS process loses nothing
		mu.Unlock()
		if counter.Add(1) == target {
			stop()
		}
		return nil
	})
	processed := counter.Load()
	w.Flush()
	if err != nil {
		// DB crash drops the connection mid-run; that's expected in this lab.
		fmt.Printf("consume ended with error (expected if Postgres was killed): %v  processed=%d\n", err, processed)
		os.Exit(0)
	}
	fmt.Printf("clean stop  processed=%d\n", processed)
}
