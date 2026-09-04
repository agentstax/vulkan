// Command outcomelab proves the four handler outcomes end to end through a
// real Consume: nil succeeds, a plain error retries, vulkan.Terminal
// dead-letters on the spot, and vulkan.Delay runs later without
// counting a failure.
//
// Registers its own topic (destroyed on exit), self-seeds four messages,
// one per branch, and reads exception_queue_<id> / delivery_log_<id> for
// the assertions.
//
// Confirms, in order:
//   - the succeeding message leaves no delivery row.
//   - the plain error's row is 'ready' with attempts 0 and a 'failure' log row.
//   - the Terminal error's row is 'dead' after ONE run, attempts 0, its
//     last_error carrying VK0055 and the wrapped cause.
//   - the Delay message's row is 'ready' with delays 1, attempts 0,
//     can_run_after in the future, and a 'delayed' log row at attempt 0.
//   - once its delay passes it runs again from the exception path; a second
//     Delay with MaxDelays 1 dead-letters it, last_error = the delay's text,
//     the retry budget still untouched (attempts - delays = 0).
//   - the plain error's retry, returning nil, deletes its row: attempts
//     climbed to 1 and nothing else about the budget moved.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"strings"
	"sync"
	"time"

	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const (
	topicName = "phase1.outcomelab"
	group     = "phase1.outcomelab"
	delay     = 2 * time.Second
)

type Payment struct {
	Branch string `json:"branch"` // "ok" | "retry" | "declined" | "settles-later"
}

func (Payment) SchemaVersion() int { return 1 }

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

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()

	tp, err := client.Topic(topicName).Register(ctx, &vulkan.TopicConfig{})
	must(err)
	defer func() {
		must(client.Topic(tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	payments, err := client.Producer(tp.Name).Register[Payment](ctx, nil)
	must(err)

	ids := map[string]int64{}
	for _, branch := range []string{"ok", "retry", "declined", "settles-later"} {
		produced, err := payments.Produce(ctx, &Payment{Branch: branch}, nil)
		must(err)
		ids[branch] = produced.Id
	}

	instance, err := client.Consumer(group, tp.Name).Register[Payment](ctx, &vulkan.ConsumerConfig{
		ExceptionInitialBackoff: 500 * time.Millisecond,
		Message: &vulkan.MessageOptions{
			Timeout: 5 * time.Second,
			Retry:   &vulkan.RetryPolicy{MaxRetries: 3, MaxDelays: 1, BaseDelay: 200 * time.Millisecond},
		},
	})

	must(err)
	groupId := groupIdOf(ctx, ds, tp.Id)

	// runs[branch] counts handler entries; the second Delay ends the lab
	var mu sync.Mutex
	runs := map[string]int{}
	consumeCtx, stop := context.WithCancel(ctx)
	defer stop()
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- instance.Consume(consumeCtx, func(ctx context.Context, payment *Payment) error {
			meta, _ := vulkan.MetaFromContext(ctx)
			mu.Lock()
			runs[payment.Branch]++
			run := runs[payment.Branch]
			mu.Unlock()
			fmt.Printf("  handler: %s run=%d meta.attempts=%d meta.delays=%d\n", payment.Branch, run, meta.Attempts, meta.Delays)

			switch payment.Branch {
			case "retry":
				if run == 1 {
					return errors.New("gateway unreachable")
				}
				return nil
			case "declined":
				return vulkan.Terminal(errors.New("issuer said no"))
			case "settles-later":
				if run == 1 && (meta.Attempts != 0 || meta.Delays != 0) {
					die(fmt.Sprintf("first run: meta.Attempts=%d meta.Delays=%d, want 0/0", meta.Attempts, meta.Delays))
				}
				if run == 2 && (meta.Attempts != 1 || meta.Delays != 1) {
					die(fmt.Sprintf("second run: meta.Attempts=%d meta.Delays=%d, want 1/1", meta.Attempts, meta.Delays))
				}
				return vulkan.Delay(delay)
			}
			return nil
		}, &vulkan.ConsumeOptions{ClaimPollRate: 100 * time.Millisecond})
	}()

	step("first delivery of all four branches")
	waitFor(ctx, ds, tp.Id, groupId, ids["settles-later"], "ready", 10*time.Second)
	waitFor(ctx, ds, tp.Id, groupId, ids["declined"], "dead", 10*time.Second)
	assertNoRow(ctx, ds, tp.Id, groupId, ids["ok"])
	fmt.Println("PASS: success left no row")

	row := readRow(ctx, ds, tp.Id, groupId, ids["retry"])
	if row.status != "ready" || row.attempts != 0 || row.delays != 0 {
		die(fmt.Sprintf("plain error row: %+v, want ready/0/0", row))
	}
	assertLogStatus(ctx, ds, tp.Id, groupId, ids["retry"], 0, "failure")
	fmt.Println("PASS: plain error -> ready, attempts 0, 'failure' log row")

	row = readRow(ctx, ds, tp.Id, groupId, ids["declined"])
	if row.status != "dead" || row.attempts != 0 || !strings.Contains(row.lastError, "[VK0055]: issuer said no") {
		die(fmt.Sprintf("terminal error row: %+v, want dead/0 with [VK0055]: issuer said no", row))
	}
	assertLogStatus(ctx, ds, tp.Id, groupId, ids["declined"], 0, "failure")
	fmt.Println("PASS: Terminal -> dead after one run, attempts 0, code and cause in last_error")

	row = readRow(ctx, ds, tp.Id, groupId, ids["settles-later"])
	if row.status != "ready" || row.attempts != 0 || row.delays != 1 || row.runsIn <= 0 {
		die(fmt.Sprintf("delayed row: %+v, want ready/0/1 with can_run_after in the future", row))
	}
	assertLogStatus(ctx, ds, tp.Id, groupId, ids["settles-later"], 0, "delayed")
	fmt.Println("PASS: Delay -> ready, delays 1, attempts 0, can_run_after ahead, 'delayed' log row")

	step("the delay passes; the exception path runs it again")
	waitFor(ctx, ds, tp.Id, groupId, ids["settles-later"], "dead", delay+10*time.Second)
	row = readRow(ctx, ds, tp.Id, groupId, ids["settles-later"])
	if row.attempts != 1 || row.delays != 1 || !strings.Contains(row.lastError, "[VK0054]") {
		die(fmt.Sprintf("delay past MaxDelays: %+v, want dead/attempts 1/delays 1 with [VK0054]", row))
	}
	assertLogStatus(ctx, ds, tp.Id, groupId, ids["settles-later"], 1, "failure")
	fmt.Println("PASS: second Delay past MaxDelays 1 -> dead, attempts - delays still 0")

	waitForGone(ctx, ds, tp.Id, groupId, ids["retry"], 10*time.Second)
	assertLogStatus(ctx, ds, tp.Id, groupId, ids["retry"], 0, "failure")
	fmt.Println("PASS: plain error's retry succeeded and its row is gone")

	stop()
	if err := <-consumeDone; err != nil && !errors.Is(err, context.Canceled) {
		must(err)
	}

	fmt.Println("\n✅ OUTCOME LAB PASSED")
	fmt.Println("   nil succeeds, a plain error retries, Terminal dead-letters on the spot,")
	fmt.Println("   and Delay runs later with its own count -- never a failure.")
	return nil
}

type queueRow struct {
	status    string
	attempts  int
	delays    int
	lastError string
	runsIn    time.Duration
}

func readRow(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64) queueRow {
	sql := fmt.Sprintf(`SELECT status, attempts, delays, COALESCE(last_error, ''), can_run_after - now() FROM %s.%s WHERE consumer_group_id = $1 AND message_id = $2`, ds.Schema, topic.ExceptionQueueTable(topicId))
	var row queueRow
	must(ds.Pool.QueryRow(ctx, sql, groupId, messageId).Scan(&row.status, &row.attempts, &row.delays, &row.lastError, &row.runsIn))
	return row
}

func rowStatus(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64) string {
	sql := fmt.Sprintf(`SELECT COALESCE(MAX(status), '') FROM %s.%s WHERE consumer_group_id = $1 AND message_id = $2`, ds.Schema, topic.ExceptionQueueTable(topicId))
	var status string
	must(ds.Pool.QueryRow(ctx, sql, groupId, messageId).Scan(&status))
	return status
}

func waitFor(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64, want string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rowStatus(ctx, ds, topicId, groupId, messageId) == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	die(fmt.Sprintf("message %d never reached status %q (last %q)", messageId, want, rowStatus(ctx, ds, topicId, groupId, messageId)))
}

func waitForGone(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if rowStatus(ctx, ds, topicId, groupId, messageId) == "" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	die(fmt.Sprintf("message %d's row never went away (last %q)", messageId, rowStatus(ctx, ds, topicId, groupId, messageId)))
}

func assertNoRow(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64) {
	if status := rowStatus(ctx, ds, topicId, groupId, messageId); status != "" {
		die(fmt.Sprintf("message %d has a delivery row with status %q, want none", messageId, status))
	}
}

func assertLogStatus(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64, attempt int, want string) {
	sql := fmt.Sprintf(`SELECT COALESCE(MAX(status), '') FROM %s.%s WHERE consumer_group_id = $1 AND message_id = $2 AND attempt = $3`, ds.Schema, topic.DeliveryLogTable(topicId))
	var status string
	must(ds.Pool.QueryRow(ctx, sql, groupId, messageId, attempt).Scan(&status))
	if status != want {
		die(fmt.Sprintf("message %d attempt %d: delivery_log status %q, want %q", messageId, attempt, status, want))
	}
}

func groupIdOf(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	var id int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT id FROM %s.consumer_group_config WHERE topic_id = $1 AND name = $2`, ds.Schema), topicId, group).Scan(&id))
	return id
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
