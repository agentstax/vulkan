package main

// Schedule permit lab: the client holds ONE SystemManager, so two
// concurrent Schedule(ctx) runs in one process refuse the second -- today's
// per-call rival manager is gone. Also proves RegisterSchedule returns the
// handle whose Get reads the declared row.

import (
	"context"
	"fmt"
	"os"
	"time"

	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
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

	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("schedulepermitlab.reports.%d", run)
	_, err = client.RegisterTopic(ctx, topicName, nil)
	must(err)

	step("RegisterSchedule returns the handle; Get reads the row")
	scheduleName := fmt.Sprintf("schedulepermitlab.nightly.%d", run)
	nightly, err := client.RegisterSchedule[ReportRequestedV1](ctx, vulkan.ScheduleSpec{Name: scheduleName, Topic: topicName, Cron: "0 3 * * *"}, &ReportRequestedV1{Kind: "nightly"}, nil)
	must(err)
	row, err := nightly.Get(ctx)
	must(err)
	if row == nil {
		die("expected the declared schedule row, got nil")
	}
	assertString("schedule name", row.Name, scheduleName)

	step("a second concurrent Schedule run is refused")
	runCtx, stopRun := context.WithCancel(ctx)
	defer stopRun()
	firstDone := make(chan error, 1)
	go func() {
		firstDone <- nightly.Schedule(runCtx)
	}()

	// give the first run its permit -- Run acquires it before any I/O
	time.Sleep(2 * time.Second)
	select {
	case err := <-firstDone:
		die(fmt.Sprintf("first Schedule run exited early: %v", err))
	default:
	}
	if err := client.Schedule(scheduleName).Schedule(ctx); err == nil {
		die("expected the second concurrent Schedule run to be refused, got nil")
	} else {
		fmt.Printf("  ✓ second run refused -> %v\n", err)
	}

	step("the first run stops clean and the permit frees")
	stopRun()
	if err := <-firstDone; err != nil {
		die(fmt.Sprintf("first Schedule run: expected nil on requested stop, got %v", err))
	}
	if err := nightly.Schedule(runCtx); err == nil {
		die("expected the run on the already-cancelled ctx to return nil error immediately, got nil error")
	} else {
		// the freed permit admits the call again -- it fails on the dead ctx, not the permit
		fmt.Printf("  ✓ permit freed, next run admitted (failed on cancelled ctx: %v)\n", err)
	}

	step("cleanup")
	must(nightly.Destroy(ctx))
	must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))

	fmt.Println("\n✅ SCHEDULE PERMIT LAB PASSED")
	return nil
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
