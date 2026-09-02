package main

import (
	"context"
	"fmt"
	"os"
	"time"

	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	metricsproducer "github.com/agentstax/vulkan/pkg/metrics/producer"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
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
	run := time.Now().UnixNano()
	topicId := run // no real topic needs to exist -- the events just carry this id as data
	group := fmt.Sprintf("abandonedroutinesnapshotlab.%d", run)

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()
	must(client.RegisterSystem(ctx, nil))

	metricsController, err := metricscontroller.NewMetricsController(ds, nil)
	must(err)

	step("never-produced (topic, group) -> zeroes, not an error")
	snapshot, err := metricsController.AbandonedRoutineSnapshot(ctx, topicId, group)
	must(err)
	assertInt64("Total", snapshot.Total, 0)
	assertInt64("Outstanding", snapshot.Outstanding, 0)
	assertDuration("SelfClearLatencyAvg", snapshot.SelfClearLatencyAvg, 0)

	step("two producers (simulating two processes) interleave abandoned/cleared for the same group")
	producerA, err := metricsproducer.NewMetricsProducer(ds, &metricsproducer.ProducerConfig{SessionFlushRate: 100 * time.Millisecond})
	must(err)
	go func() {
		must(producerA.Run(ctx, group, "abandonedroutinesnapshotlab", 1, "session-a"))
	}()
	producerB, err := metricsproducer.NewMetricsProducer(ds, &metricsproducer.ProducerConfig{SessionFlushRate: 100 * time.Millisecond})
	must(err)
	go func() {
		must(producerB.Run(ctx, group, "abandonedroutinesnapshotlab", 1, "session-b"))
	}()

	producerA.RecordAbandoned(topicId, group, 1, 1) // matched pair, cleared by A
	producerB.RecordAbandoned(topicId, group, 2, 1) // matched pair, cleared by B
	time.Sleep(20 * time.Millisecond)               // let the self-clear latency be non-zero and measurable
	producerA.RecordCleared(topicId, group, 1, 1)
	producerB.RecordCleared(topicId, group, 2, 1)
	producerA.RecordAbandoned(topicId, group, 3, 1) // never cleared -- outstanding

	// events are produced off the hot path, landing on the next flush tick --
	// give them a moment to actually land
	must(waitFor(10*time.Second, func() (bool, error) {
		s, err := metricsController.AbandonedRoutineSnapshot(ctx, topicId, group)
		if err != nil {
			return false, err
		}
		return s.Total == 3, nil
	}))

	snapshot, err = metricsController.AbandonedRoutineSnapshot(ctx, topicId, group)
	must(err)
	assertInt64("Total", snapshot.Total, 3)
	assertInt64("Outstanding", snapshot.Outstanding, 1)
	if snapshot.SelfClearLatencyAvg <= 0 {
		die(fmt.Sprintf("SelfClearLatencyAvg: expected > 0, got %v", snapshot.SelfClearLatencyAvg))
	}
	fmt.Printf("  ✓ SelfClearLatencyAvg (%v)\n", snapshot.SelfClearLatencyAvg)

	step("a different group on the same topic id sees none of the above")
	otherGroup := fmt.Sprintf("abandonedroutinesnapshotlab.other.%d", run)
	isolated, err := metricsController.AbandonedRoutineSnapshot(ctx, topicId, otherGroup)
	must(err)
	assertInt64("Total", isolated.Total, 0)
	assertInt64("Outstanding", isolated.Outstanding, 0)

	fmt.Println("\n✅ ABANDONED ROUTINE SNAPSHOT LAB PASSED")
	return nil
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

func assertDuration(label string, got, want time.Duration) {
	if got != want {
		die(fmt.Sprintf("%s: got %v, want %v", label, got, want))
	}
	fmt.Printf("  ✓ %s (%v)\n", label, got)
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
