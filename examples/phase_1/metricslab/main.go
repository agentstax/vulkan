package main

// Chunk 5 lab: TWO independent MessageConsumer instances (simulating two
// processes) share ONE consumer group on ONE topic. Each hard-times-out
// whatever it claims, proving the abandoned/cleared event stream aggregates
// correctly ACROSS processes -- neither instance's in-memory state is ever
// consulted, everything the assertions below read comes back out of the
// shared __system.metrics topic via admin.TopicMetrics, the same read path
// `vulkan topic get` renders.
//
// Retention-drop-out (events aging out of the window) is NOT exercised here:
// __system.metrics is a single shared, already-populated topic with a fixed
// 10,000-row partition size (PartitionSize is immutable after creation), so
// forcing a partition boundary in a short-lived lab isn't practical without
// either a huge event volume or a second, parallel metrics topic -- neither
// of which this design supports. The read path applies no separate time
// filter of its own (see pkg/metrics/controller/datastore/event.go) -- once a
// partition is physically dropped its rows are just gone from every query,
// so there's no additional logic path here that could get that wrong.

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/admin"
	iCommon "github.com/agentstax/vulkan/pkg/common"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	consumermessage "github.com/agentstax/vulkan/pkg/consumer/message"
	"github.com/agentstax/vulkan/pkg/consumer/messageconsumer"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	iMetrics "github.com/agentstax/vulkan/pkg/metrics"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
	"github.com/google/uuid"
)

const group = "metricslab"

func main() {
	ctx := context.Background()
	run := time.Now().UnixNano()

	ds, err := iDatastore.NewPostgresDatastore(ctx, &iDatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("metricslab.%d", run)
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	wp, err := producer.NewProducer[common.Work](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)
	for range 4 {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}

	gates := newReleaseGates()
	consumerFunc := func(ctx context.Context, work *common.Work) error {
		meta, _ := consumermessage.MetaFromContext(ctx)
		<-gates.wait(meta.Id) // never returns until released -- simulates a stuck goroutine
		return nil
	}

	cfg := &messageconsumer.MessageConsumerConfig{
		BatchLimit:         2,
		QueueSize:          10,
		MessageConcurrency: 2,
		Message:            &iCommon.MessageOptions{Timeout: 300 * time.Millisecond},
		TimeoutGrace:       100 * time.Millisecond,
		QueueMargin:        200 * time.Millisecond,
		AckMargin:          200 * time.Millisecond,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	consumerDatastore, err := consumercontroller.NewConsumerController(ds, nil)
	must(err)
	g, err := consumerDatastore.RegisterGroup(ctx, tp.Id, group)
	must(err)
	owner, err := iCommon.NewConsumerGroupOwner(tp.SystemId, tp.Id, g.Id, g.Name)
	must(err)
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)

	step("two independent consumer processes claim the same group's cursor")
	var wg sync.WaitGroup
	// consumer rows carry no instance target, so both "processes" claim a life
	// of the same row
	startConsumer := func(label string) {
		abandonedEvents, err := consumermetrics.NewMetricEventProducer(ds, &consumermetrics.MetricEventConfig{})
		must(err)
		go func() { must(abandonedEvents.Run(runCtx)) }()

		definition, err := messageconsumer.NewMessageConsumerDefinition(ds, consumerFunc, abandonedEvents, cfg)
		must(err)
		must(definition.Declare(runCtx, owner))

		row, err := workers.GetWorker(runCtx, definition.Name(), owner)
		must(err)
		execution, err := definition.Provision(runCtx, row.Id, &row.Owner, row.Metadata)
		must(err)

		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := execution.Run(runCtx); err != nil && runCtx.Err() == nil {
				die(fmt.Sprintf("%s: Run returned %v", label, err))
			}
		}()
	}
	startConsumer("process A")
	startConsumer("process B")

	step("wait for all 4 messages to hard-timeout across both processes")
	must(waitFor(10*time.Second, func() (bool, error) {
		snap, err := topicMetrics(ctx, mAdmin, topicName)
		if err != nil || snap == nil || len(snap.Groups) == 0 {
			return false, err
		}
		return snap.Groups[0].AbandonedRoutines.Total == 4, nil
	}))
	snap := mustTopicMetrics(ctx, mAdmin, topicName)
	assertInt64("Total abandoned across both processes", snap.Groups[0].AbandonedRoutines.Total, 4)
	assertInt64("Outstanding (nothing cleared yet)", snap.Groups[0].AbandonedRoutines.Outstanding, 4)

	step("release 2 of the 4 -- outstanding falls, self-clear latency becomes measurable")
	gates.release(1)
	gates.release(2)
	must(waitFor(10*time.Second, func() (bool, error) {
		snap := mustTopicMetrics(ctx, mAdmin, topicName)
		return snap.Groups[0].AbandonedRoutines.Outstanding == 2, nil
	}))
	snap = mustTopicMetrics(ctx, mAdmin, topicName)
	assertInt64("Total unchanged", snap.Groups[0].AbandonedRoutines.Total, 4)
	assertInt64("Outstanding falls to 2", snap.Groups[0].AbandonedRoutines.Outstanding, 2)
	if snap.Groups[0].AbandonedRoutines.SelfClearLatencyAvg <= 0 {
		die("expected SelfClearLatencyAvg > 0 once some events cleared")
	}
	fmt.Printf("  ✓ SelfClearLatencyAvg (%v)\n", snap.Groups[0].AbandonedRoutines.SelfClearLatencyAvg)

	step("mAdmin.TopicMetrics is the same read `vulkan topic get` renders -- cursor/exception state came back too")
	fmt.Printf("  ✓ cursor backlog=%d, ready exceptions=%d\n",
		snap.Groups[0].Cursor.Backlog, snap.Groups[0].Exceptions.Ready)

	cancel()
	wg.Wait()

	fmt.Println("\n✅ METRICS LAB PASSED")
}

// ---- release gates: lets consumerFunc block per-message until the test says go ----

type releaseGates struct {
	mu    sync.Mutex
	gates map[int64]chan struct{}
}

func newReleaseGates() *releaseGates {
	return &releaseGates{gates: make(map[int64]chan struct{})}
}

func (g *releaseGates) gate(id int64) chan struct{} {
	g.mu.Lock()
	defer g.mu.Unlock()
	ch, ok := g.gates[id]
	if !ok {
		ch = make(chan struct{})
		g.gates[id] = ch
	}
	return ch
}

func (g *releaseGates) wait(id int64) <-chan struct{} { return g.gate(id) }
func (g *releaseGates) release(id int64)              { close(g.gate(id)) }

// ---- helpers ----

func topicMetrics(ctx context.Context, mAdmin *admin.MessageAdmin, name string) (*iMetrics.TopicSnapshot, error) {
	return mAdmin.TopicMetrics(ctx, name, topic.SchemaVersion(1))
}

func mustTopicMetrics(ctx context.Context, mAdmin *admin.MessageAdmin, name string) *iMetrics.TopicSnapshot {
	snap, err := topicMetrics(ctx, mAdmin, name)
	must(err)
	if len(snap.Groups) == 0 {
		die("expected at least one bound group")
	}
	return snap
}

func waitFor(timeout time.Duration, cond func() (bool, error)) error {
	deadline := time.Now().Add(timeout)
	for {
		ok, err := cond()
		if err != nil {
			return err
		}
		if ok {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for condition")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertInt64(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
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
