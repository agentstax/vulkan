package main

// Schedule concurrency lab: Schedule(ctx) runs the system manager, so two
// concurrent runs in one process are both admitted and the manager row's
// claim gate is what admits one reconcile loop between them. Also proves
// RegisterSchedule returns the handle whose Get reads the declared row.

import (
	"context"
	"fmt"
	"os"
	"time"

	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

type ReportRequestedV1 struct {
	Kind string `json:"kind"`
}

func (ReportRequestedV1) SchemaVersion() int { return 1 }

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

	topicName := fmt.Sprintf("scheduleconcurrencylab.reports.%d", run)
	_, err = client.RegisterTopic(ctx, topicName, nil)
	must(err)

	step("RegisterSchedule returns the handle; Get reads the row")
	scheduleName := fmt.Sprintf("scheduleconcurrencylab.nightly.%d", run)
	nightly, err := client.RegisterSchedule[ReportRequestedV1](ctx, &vulkan.ScheduleSpec{Name: scheduleName, Topic: topicName, Cron: "0 3 * * *"}, &ReportRequestedV1{Kind: "nightly"}, nil)
	must(err)
	row, err := nightly.Get(ctx)
	must(err)
	if row == nil {
		die("expected the declared schedule row, got nil")
	}
	assertString("schedule name", row.Name, scheduleName)

	step("two concurrent Schedule runs are both admitted")
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- nightly.Schedule(runCtx)
	}()

	secondCtx, stopSecond := context.WithCancel(ctx)
	defer stopSecond()
	secondDone := make(chan error, 1)
	go func() {
		secondDone <- client.Schedule(scheduleName).Schedule(secondCtx)
	}()

	time.Sleep(5 * time.Second)
	select {
	case err := <-firstDone:
		die(fmt.Sprintf("first Schedule run exited early: %v", err))
	case err := <-secondDone:
		die(fmt.Sprintf("second Schedule run was refused: %v", err))
	default:
	}
	live := scalar(ctx, client, `
		SELECT count(*)
		FROM %[1]s.worker_instance i
		JOIN %[1]s.worker_config w ON w.id = i.worker_id
		WHERE w.name = 'manager'
			AND w.system_id IS NOT NULL
			AND i.expires_at > now()`)
	if live != 1 {
		die(fmt.Sprintf("%d live system manager instances, want 1 -- the row's claim gate admits one, so an installation created before the gate (target_instances -1) needs a drop+recreate of its schema", live))
	}
	fmt.Println("  ✓ both runs admitted, one live manager instance between them")

	step("each run stops clean on its own ctx")
	stopSecond()
	if err := <-secondDone; err != nil {
		die(fmt.Sprintf("second Schedule run: expected nil on requested stop, got %v", err))
	}
	stopRun()
	if err := <-firstDone; err != nil {
		die(fmt.Sprintf("first Schedule run: expected nil on requested stop, got %v", err))
	}
	fmt.Println("  ✓ both returned nil on a requested stop")

	step("cleanup")
	must(nightly.Destroy(ctx))
	must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))

	fmt.Println("\n✅ SCHEDULE CONCURRENCY LAB PASSED")
	return nil
}

// scalar runs a one-value query whose every table name is the client's own
// schema at verb [1].
func scalar(ctx context.Context, client *vulkan.Client, sql string) int64 {
	var value int64
	must(client.Datastore().Pool.QueryRow(ctx, fmt.Sprintf(sql, client.Datastore().Schema)).Scan(&value))
	return value
}

func assertString(label string, got string, want string) {
	if got != want {
		die(fmt.Sprintf("%s: got %q, want %q", label, got, want))
	}
	fmt.Printf("  ✓ %s (%q)\n", label, got)
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
