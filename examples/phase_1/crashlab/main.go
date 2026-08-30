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
	"errors"
	"flag"
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
)

func main() {
	if err := run(); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run() error {
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
		return errors.New("-count must be >= 1")
	}

	logFile, err := os.OpenFile(*logPtr, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	w := bufio.NewWriter(logFile)

	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	time.AfterFunc(180*time.Second, stop) // watchdog

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{
		Pass: "example_password", MaxConns: *maxConnsPtr,
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

	// Short lease (= Timeout+QueueMargin+RecordMargin = 4s) so in-flight rows
	// reclaim quickly after the crash. High MaxRetries so reprocessing never
	// dead-letters — we want pure at-least-once redelivery, not the DLQ path.
	wc, err := consumer.NewConsumer[common.Work](ds, &consumer.ConsumerConfig{
		BatchLimit:         100,
		QueueSize:          100 + conc,
		MessageConcurrency: conc,
		Message:            &iCommon.MessageOptions{Timeout: 2 * time.Second, Retry: &iCommon.RetryPolicy{MaxRetries: 100}},
		ClaimPollRate:      200 * time.Millisecond,
		QueueMargin:        1 * time.Second,
		RecordMargin:       1 * time.Second,
	})
	if err != nil {
		return err
	}

	wcInstance, err := wc.Register(ctx, *groupPtr, t.Name, nil)
	if err != nil {
		return err
	}

	var mu sync.Mutex
	var counter atomic.Int64

	err = wcInstance.Consume(ctx, func(ctx context.Context, work *common.Work) error {
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
		return nil
	}
	fmt.Printf("clean stop  processed=%d\n", processed)
	return nil
}
