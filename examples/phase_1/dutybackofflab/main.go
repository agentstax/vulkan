// Command dutybackofflab proves a consistently-failing worker backs off
// instead of ticking at full poll rate forever.
//
// Renames a topic's message_log_<id> table out from under a running janitor
// worker, so every sweep fails 42P01. Watches the claimed worker_instance's
// `attempts` streak climb and the gap between failures grow -- small at
// first, capped at SweepRetry's MaxDelay -- then renames the table back and
// confirms the next successful sweep resets attempts to 0.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	metricscontroller "github.com/agentstax/vulkan/pkg/metrics/controller"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/topic/janitor"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
)

const (
	pollRate    = 50 * time.Millisecond
	backoffBase = 300 * time.Millisecond
	backoffMax  = 1500 * time.Millisecond
)

func main() {
	ctx := context.Background()

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("dutybackofflab.%d", time.Now().UnixNano())
	// retention on: the sweep's drop pass reads message_log's head every tick,
	// which is the read the rename below breaks
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{RetentionTTL: time.Hour})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	janitorProvisioner, err := janitor.NewJanitorProvisioner(ds, &janitor.JanitorConfig{
		SweepRetry: &common.RetryPolicy{BaseDelay: backoffBase, MaxDelay: backoffMax},
	})
	must(err)
	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)

	// RegisterTopic already declared the janitor row -- claim it directly with
	// the lab's own fast tick
	owner, err := common.NewTopicOwner(tp.SystemId, tp.Id, tp.Name)
	must(err)
	row, err := workers.GetWorker(ctx, janitor.WorkerJanitor, owner)
	must(err)
	row.Metadata = map[string]any{
		"poll_rate":        int64(pollRate),
		"sweep_batch_size": 1000,
	}
	execution, err := janitorProvisioner.Provision(ctx, row)
	must(err)
	if execution == nil {
		die("janitor declined the instance -- is another claimant running?")
	}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- execution.Run(runCtx) }()

	table := fmt.Sprintf("message_log_%d", tp.Id)
	hidden := table + "_hidden"

	step("breaking the janitor: renaming its message_log table away")
	exec(ctx, ds, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, table, hidden))

	step("watching attempts climb, the gap between failures growing but capped at SweepRetry's MaxDelay")
	type sample struct {
		attempts int
		at       time.Duration
	}
	start := time.Now()
	var samples []sample
	deadline := start.Add(15 * time.Second)
	for time.Now().Before(deadline) {
		attempts := scalarInt(ctx, ds, `SELECT attempts FROM worker_instance WHERE worker_id=$1`, row.Id)
		if len(samples) == 0 || attempts != samples[len(samples)-1].attempts {
			samples = append(samples, sample{attempts, time.Since(start)})
			fmt.Printf("  attempts=%d at %v\n", attempts, time.Since(start).Round(time.Millisecond))
		}
		if attempts >= 5 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if len(samples) < 6 {
		die(fmt.Sprintf("expected attempts to climb to at least 5, got samples: %+v", samples))
	}

	firstGap := samples[2].at - samples[1].at
	lastGap := samples[len(samples)-1].at - samples[len(samples)-2].at
	fmt.Printf("  ✓ first gap %v, last gap %v (cap %v)\n", firstGap, lastGap, backoffMax)
	if firstGap >= backoffBase*3 {
		die(fmt.Sprintf("first backoff gap %v looked capped already, want close to SweepRetry's BaseDelay (%v)", firstGap, backoffBase))
	}
	if lastGap <= firstGap {
		die(fmt.Sprintf("backoff gap didn't grow: first=%v last=%v", firstGap, lastGap))
	}
	if lastGap > backoffMax+pollRate*4 {
		die(fmt.Sprintf("backoff gap %v exceeded SweepRetry's MaxDelay (%v) by more than jitter/poll slack", lastGap, backoffMax))
	}

	step("confirming WorkerSnapshots surfaces the failing streak")
	metricsController, err := metricscontroller.NewMetricsController(ds, nil)
	must(err)
	snapshots, err := metricsController.WorkerSnapshots(ctx)
	must(err)
	found := false
	for _, s := range snapshots {
		if s.Owner.Name == topicName && s.Name == janitor.WorkerJanitor {
			found = true
			if s.Attempts == 0 {
				die("expected WorkerSnapshot.Attempts > 0 for the failing janitor")
			}
			fmt.Printf("  ✓ WorkerSnapshot: status=%s attempts=%d\n", s.Status, s.Attempts)
		}
	}
	if !found {
		die("janitor worker not found in WorkerSnapshots")
	}

	step("healing: renaming the table back and waiting for attempts to reset")
	exec(ctx, ds, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, hidden, table))

	deadline = time.Now().Add(10 * time.Second)
	var final int
	for time.Now().Before(deadline) {
		final = scalarInt(ctx, ds, `SELECT attempts FROM worker_instance WHERE worker_id=$1`, row.Id)
		if final == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if final != 0 {
		die(fmt.Sprintf("expected attempts to reset to 0 after recovery, got %d", final))
	}
	fmt.Println("  ✓ attempts reset to 0 after a successful sweep")

	cancel()
	must(<-done)

	fmt.Println("\n✅ DUTY BACKOFF LAB PASSED")
}

func exec(ctx context.Context, ds *iDatastore.PostgresDatastore, sql string) {
	_, err := ds.Pool.Exec(ctx, sql)
	must(err)
}

func scalarInt(ctx context.Context, ds *iDatastore.PostgresDatastore, q string, args ...any) int {
	var v int
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func step(s string) { fmt.Printf("\n--- %s ---\n", s) }

func must(err error) {
	if err != nil {
		die(err.Error())
	}
}

func die(msg string) {
	fmt.Println("❌ " + msg)
	os.Exit(1)
}
