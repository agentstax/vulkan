// Command dutybackofflab proves a consistently-erroring duty backs off
// instead of retrying at full poll rate forever.
//
// Renames a topic's message_log_<id> table out from under a running janitor
// duty, so every sweep fails 42P01. Watches the maintenance row's `attempts`
// climb and the gap between claims grow -- small at first, capped at
// DutyRetry's MaxDelay -- then renames the table back and confirms the next
// successful run resets attempts to 0.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/maintain"
	metricsdatastore "github.com/agentstax/vulkan/pkg/metrics/datastore"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/agentstax/vulkan/pkg/topic"
)

const (
	dutyRate    = 50 * time.Millisecond
	backoffBase = 300 * time.Millisecond
	backoffMax  = 1500 * time.Millisecond
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("dutybackofflab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topic.Config{
		JanitorPollRate: dutyRate,
	})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	}()

	j, err := maintain.NewJanitor(topicName, topic.SchemaVersion(1), ds, &maintain.MaintainerConfig{
		DutyRetry: &retry.Policy{BaseDelay: backoffBase, MaxDelay: backoffMax},
	})
	must(err)
	must(j.Register(ctx))

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- j.Run(runCtx) }()

	table := fmt.Sprintf("message_log_%d", tp.Id)
	hidden := table + "_hidden"

	step("breaking the janitor: renaming its message_log table away")
	exec(ctx, ds, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, table, hidden))

	step("watching attempts climb, the gap between claims growing but capped at DutyRetry's MaxDelay")
	type sample struct {
		attempts int
		at       time.Duration
	}
	start := time.Now()
	var samples []sample
	deadline := start.Add(15 * time.Second)
	for time.Now().Before(deadline) {
		attempts := scalarInt(ctx, ds, `SELECT attempts FROM maintenance WHERE duty='janitor' AND topic_id=$1`, tp.Id)
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
		die(fmt.Sprintf("first backoff gap %v looked capped already, want close to DutyRetry's BaseDelay (%v)", firstGap, backoffBase))
	}
	if lastGap <= firstGap {
		die(fmt.Sprintf("backoff gap didn't grow: first=%v last=%v", firstGap, lastGap))
	}
	if lastGap > backoffMax+dutyRate*4 {
		die(fmt.Sprintf("backoff gap %v exceeded DutyRetry's MaxDelay (%v) by more than jitter/poll slack", lastGap, backoffMax))
	}

	step("confirming DutySnapshots surfaces the failing streak")
	md, err := metricsdatastore.NewMetricsDatastore(ds, nil)
	must(err)
	duties, err := md.DutySnapshots(ctx)
	must(err)
	found := false
	for _, d := range duties {
		if d.TopicName == topicName && d.Duty == "janitor" {
			found = true
			if d.Attempts == 0 {
				die("expected DutySnapshot.Attempts > 0 for the failing janitor")
			}
			fmt.Printf("  ✓ DutySnapshot: attempts=%d overdue=%v\n", d.Attempts, d.Overdue)
		}
	}
	if !found {
		die("janitor duty not found in DutySnapshots")
	}

	step("healing: renaming the table back and waiting for attempts to reset")
	exec(ctx, ds, fmt.Sprintf(`ALTER TABLE %s RENAME TO %s`, hidden, table))

	deadline = time.Now().Add(10 * time.Second)
	var final int
	for time.Now().Before(deadline) {
		final = scalarInt(ctx, ds, `SELECT attempts FROM maintenance WHERE duty='janitor' AND topic_id=$1`, tp.Id)
		if final == 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if final != 0 {
		die(fmt.Sprintf("expected attempts to reset to 0 after recovery, got %d", final))
	}
	fmt.Println("  ✓ attempts reset to 0 after a successful run")

	cancel()
	must(<-done)

	fmt.Println("\n✅ DUTY BACKOFF LAB PASSED")
}

func exec(ctx context.Context, ds *coredatastore.PostgresDatastore, sql string) {
	_, err := ds.Pool.Exec(ctx, sql)
	must(err)
}

func scalarInt(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int {
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
