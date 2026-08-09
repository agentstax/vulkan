package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
)

func main() {
	ctx := context.Background()
	run := time.Now().UnixNano()
	topicID := run // no real topic needs to exist -- the events just carry this id as data
	group := fmt.Sprintf("eventsnapshotlab.%d", run)

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	metricsController, err := metricscontroller.NewMetricsController(ds, nil)
	must(err)

	step("never-produced (topic, group) -> zeroes, not an error")
	snapshot, err := metricsController.AbandonedRoutineSnapshot(ctx, topicID, group)
	must(err)
	assertInt64("Total", snapshot.Total, 0)
	assertInt64("Outstanding", snapshot.Outstanding, 0)
	assertDuration("SelfClearLatencyAvg", snapshot.SelfClearLatencyAvg, 0)

	step("two producers (simulating two processes) interleave abandoned/cleared for the same group")
	producerA, err := consumermetrics.NewMetricEventProducer(ds, nil)
	must(err)
	go func() { must(producerA.Run(ctx)) }()
	producerB, err := consumermetrics.NewMetricEventProducer(ds, nil)
	must(err)
	go func() { must(producerB.Run(ctx)) }()

	producerA.Add(ctx, topicID, group, 1, 1) // matched pair, cleared by A
	producerB.Add(ctx, topicID, group, 2, 1) // matched pair, cleared by B
	time.Sleep(20 * time.Millisecond)        // let the self-clear latency be non-zero and measurable
	producerA.Remove(ctx, topicID, group, 1, 1)
	producerB.Remove(ctx, topicID, group, 2, 1)
	producerA.Add(ctx, topicID, group, 3, 1) // never cleared -- outstanding

	// events are produced off the hot path via a buffered channel drained by
	// a background goroutine -- give it a moment to actually land
	must(waitFor(10*time.Second, func() (bool, error) {
		s, err := metricsController.AbandonedRoutineSnapshot(ctx, topicID, group)
		if err != nil {
			return false, err
		}
		return s.Total == 3, nil
	}))

	snapshot, err = metricsController.AbandonedRoutineSnapshot(ctx, topicID, group)
	must(err)
	assertInt64("Total", snapshot.Total, 3)
	assertInt64("Outstanding", snapshot.Outstanding, 1)
	if snapshot.SelfClearLatencyAvg <= 0 {
		die(fmt.Sprintf("SelfClearLatencyAvg: expected > 0, got %v", snapshot.SelfClearLatencyAvg))
	}
	fmt.Printf("  ✓ SelfClearLatencyAvg (%v)\n", snapshot.SelfClearLatencyAvg)

	step("a different group on the same topic id sees none of the above")
	otherGroup := fmt.Sprintf("eventsnapshotlab.other.%d", run)
	isolated, err := metricsController.AbandonedRoutineSnapshot(ctx, topicID, otherGroup)
	must(err)
	assertInt64("Total", isolated.Total, 0)
	assertInt64("Outstanding", isolated.Outstanding, 0)

	fmt.Println("\n✅ EVENT SNAPSHOT LAB PASSED")
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
	fmt.Printf("\n❌ LAB FAILED: %s\n", msg)
	os.Exit(1)
}
