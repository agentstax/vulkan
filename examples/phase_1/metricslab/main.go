package main

// Chunk 5 lab: TWO independent MessageConsumer instances (simulating two
// processes) share ONE consumer group on ONE topic. Each hard-times-out
// whatever it claims, proving the abandoned/cleared event stream aggregates
// correctly ACROSS processes -- neither instance's in-memory state is ever
// consulted, everything the assertions below read comes back out of the
// shared __system.metrics topic via client.Topic(...).Metrics, the same read path
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
	iCommon "github.com/agentstax/vulkan/pkg/common"
	consumermessage "github.com/agentstax/vulkan/pkg/consumergroup"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	"github.com/agentstax/vulkan/pkg/consumergroup/messageconsumer"
	iMetrics "github.com/agentstax/vulkan/pkg/metrics"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

const group = "metricslab"

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
	run := time.Now().UnixNano()

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()

	topicName := fmt.Sprintf("metricslab.%d", run)
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{})
	must(err)
	defer func() {
		must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	wpInstance, err := client.RegisterProducer[common.Work](ctx, tp.Name, nil)
	must(err)
	for range 4 {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, nil)
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
		RecordMargin:       200 * time.Millisecond,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	consumerDatastore, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	g, err := consumerDatastore.RegisterGroup(ctx, tp.Id, group, consumermessage.Beginning())
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
		abandonedEvents, err := metricsproducer.NewMetricsProducer(ds, &metricsproducer.ProducerConfig{SessionFlushRate: 100 * time.Millisecond})
		must(err)
		go func() { must(abandonedEvents.Run(runCtx, g.Name, tp.Name, 1, label)) }()

		provisioner, err := messageconsumer.NewMessageConsumerProvisioner(ds, consumerFunc, 1, abandonedEvents, cfg)
		must(err)
		must(provisioner.Declare(runCtx, owner))

		row, err := workers.GetWorker(runCtx, provisioner.Definition().Name, owner)
		must(err)
		execution, err := provisioner.Provision(runCtx, row)
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
		snap, err := topicMetrics(ctx, client, topicName)
		if err != nil || snap == nil || len(snap.Groups) == 0 {
			return false, err
		}
		return snap.Groups[0].AbandonedRoutines.Total == 4, nil
	}))
	snap := mustTopicMetrics(ctx, client, topicName)
	assertInt64("Total abandoned across both processes", snap.Groups[0].AbandonedRoutines.Total, 4)
	assertInt64("Outstanding (nothing cleared yet)", snap.Groups[0].AbandonedRoutines.Outstanding, 4)

	step("release 2 of the 4 -- outstanding falls, self-clear latency becomes measurable")
	gates.release(1)
	gates.release(2)
	must(waitFor(10*time.Second, func() (bool, error) {
		snap := mustTopicMetrics(ctx, client, topicName)
		return snap.Groups[0].AbandonedRoutines.Outstanding == 2, nil
	}))
	snap = mustTopicMetrics(ctx, client, topicName)
	assertInt64("Total unchanged", snap.Groups[0].AbandonedRoutines.Total, 4)
	assertInt64("Outstanding falls to 2", snap.Groups[0].AbandonedRoutines.Outstanding, 2)
	if snap.Groups[0].AbandonedRoutines.SelfClearLatencyAvg <= 0 {
		die("expected SelfClearLatencyAvg > 0 once some events cleared")
	}
	fmt.Printf("  ✓ SelfClearLatencyAvg (%v)\n", snap.Groups[0].AbandonedRoutines.SelfClearLatencyAvg)

	step("client.Topic(...).Metrics is the same read `vulkan topic get` renders -- cursor/exception state came back too")
	fmt.Printf("  ✓ cursor backlog=%d, ready exceptions=%d\n",
		snap.Groups[0].Cursor.Backlog, snap.Groups[0].Exceptions.Ready)

	cancel()
	wg.Wait()

	fmt.Println("\n✅ METRICS LAB PASSED")
	return nil
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

func topicMetrics(ctx context.Context, client *vulkan.Client, name string) (*iMetrics.TopicSnapshot, error) {
	return client.Topic(name).Metrics(ctx)
}

func mustTopicMetrics(ctx context.Context, client *vulkan.Client, name string) *iMetrics.TopicSnapshot {
	snap, err := topicMetrics(ctx, client, name)
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
	panic(labFailure{message: msg})
}
