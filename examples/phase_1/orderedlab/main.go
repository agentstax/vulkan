// Command orderedlab proves ConcurrencyOrdered end to end through a real
// Consume: same-key messages run in id order, one at a time, and the order
// holds through a failure -- the failed message's retry goes before the
// messages behind it -- while a dead-lettered predecessor releases the lane.
//
// Registers its own topic (destroyed on exit), produces keyed messages under
// MessageConcurrency 4 so a plain exclusive policy would let the later ones
// run first, and reads exception_queue_<id> for the assertions.
//
// Confirms, in order:
//   - produce refuses ordered without a key, and ordered with compaction.
//   - key acct-1: three messages; the first fails once. The handler sees
//     1, 1 (retry), 2, 3 -- never 2 or 3 before the retry succeeded -- and
//     rows 2 and 3 sat 'deferred' while 1 was 'ready'.
//   - key acct-2: the first is Terminal; the second runs after it is 'dead'.
//   - a different key (acct-3) is not held behind either lane.
//   - key acct-4: twenty messages in one range, none failing, run in order
//     back to back on the fast path -- no deferred row, no exception poll.
package main

import (
	"context"
	"errors"
	"fmt"
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"sync"
	"time"

	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const (
	topicName = "phase1.orderedlab"
	group     = "phase1.orderedlab"
)

type Adjustment struct {
	Account string `json:"account"`
	Seq     int    `json:"seq"`
}

func (Adjustment) SchemaVersion() int { return 1 }

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

	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{})
	must(err)
	defer func() {
		must(client.Topic(tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	adjustments, err := client.RegisterProducer[Adjustment](ctx, tp.Name, nil)
	must(err)

	step("produce-time guards")
	ordered := &vulkan.MessageOptions{Concurrency: vulkan.ConcurrencyOrdered}
	if _, err := adjustments.Produce(ctx, &Adjustment{Account: "acct-0"}, &vulkan.ProduceOptions{Message: ordered}); err == nil {
		die("ordered without a MessageKey must be refused")
	}
	if _, err := adjustments.Produce(ctx, &Adjustment{Account: "acct-0"}, &vulkan.ProduceOptions{MessageKey: "acct-0", Message: ordered, Compaction: &vulkan.CompactionOptions{Enable: true}}); err == nil {
		die("ordered with Compaction enabled must be refused")
	}
	fmt.Println("PASS: ordered needs a key and refuses compaction")

	ids := map[string]int64{}
	produce := func(account string, seq int) {
		produced, err := adjustments.Produce(ctx, &Adjustment{Account: account, Seq: seq}, &vulkan.ProduceOptions{MessageKey: account, Message: ordered})
		must(err)
		ids[fmt.Sprintf("%s/%d", account, seq)] = produced.Id
	}
	produce("acct-1", 1)
	produce("acct-1", 2)
	produce("acct-1", 3)
	produce("acct-2", 1)
	produce("acct-2", 2)
	produce("acct-3", 1)
	for seq := 1; seq <= 20; seq++ {
		produce("acct-4", seq)
	}

	instance, err := client.RegisterConsumer[Adjustment](ctx, group, tp.Name, &vulkan.ConsumerConfig{
		ExceptionInitialBackoff: 500 * time.Millisecond,
		Message: &vulkan.MessageOptions{
			Timeout: 5 * time.Second,
			Retry:   &vulkan.RetryPolicy{MaxRetries: 3, BaseDelay: 200 * time.Millisecond},
		},
	})
	must(err)
	groupId := groupIdOf(ctx, ds, tp.Id)

	// seen[account] is the sequence of Seq values the handler was entered with
	var mu sync.Mutex
	seen := map[string][]int{}
	consumeCtx, stop := context.WithCancel(ctx)
	defer stop()
	consumeDone := make(chan error, 1)
	go func() {
		consumeDone <- instance.Consume(consumeCtx, func(ctx context.Context, adjustment *Adjustment) error {
			mu.Lock()
			seen[adjustment.Account] = append(seen[adjustment.Account], adjustment.Seq)
			runs := len(seen[adjustment.Account])
			mu.Unlock()
			fmt.Printf("  handler: %s seq=%d (entry %d for the key)\n", adjustment.Account, adjustment.Seq, runs)

			switch {
			case adjustment.Account == "acct-1" && adjustment.Seq == 1 && runs == 1:
				return errors.New("ledger unavailable")
			case adjustment.Account == "acct-2" && adjustment.Seq == 1:
				return vulkan.Terminal(errors.New("account closed"))
			}
			return nil
		}, &vulkan.ConsumeOptions{
			BatchLimit:         50,
			MessageConcurrency: 4,
			ClaimPollRate:      100 * time.Millisecond,
		})
	}()

	entries := func(account string) []int {
		mu.Lock()
		defer mu.Unlock()
		return append([]int(nil), seen[account]...)
	}

	step("acct-1: a failure holds the lane; the retry runs before 2 and 3")
	waitUntil(func() bool { return len(entries("acct-1")) >= 4 }, 20*time.Second, "acct-1 to be entered four times")
	waitForGone(ctx, ds, tp.Id, groupId, ids["acct-1/3"], 10*time.Second)
	got := entries("acct-1")
	if fmt.Sprint(got) != "[1 1 2 3]" {
		die(fmt.Sprintf("acct-1 handler order %v, want [1 1 2 3]", got))
	}
	assertLogStatus(ctx, ds, tp.Id, groupId, ids["acct-1/2"], 0, "deferred")
	assertLogStatus(ctx, ds, tp.Id, groupId, ids["acct-1/3"], 0, "deferred")
	fmt.Println("PASS: 1 failed, 2 and 3 were deferred, the retry of 1 ran first")

	step("acct-2: a dead predecessor releases the lane")
	waitFor(ctx, ds, tp.Id, groupId, ids["acct-2/1"], "dead", 10*time.Second)
	waitUntil(func() bool { return len(entries("acct-2")) >= 2 }, 20*time.Second, "acct-2 to be entered twice")
	waitForGone(ctx, ds, tp.Id, groupId, ids["acct-2/2"], 10*time.Second)
	got = entries("acct-2")
	acct3 := len(entries("acct-3"))
	if fmt.Sprint(got) != "[1 2]" {
		die(fmt.Sprintf("acct-2 handler order %v, want [1 2]", got))
	}
	if acct3 != 1 {
		die(fmt.Sprintf("acct-3 ran %d times, want 1", acct3))
	}
	fmt.Println("PASS: 2 ran after 1 was dead-lettered; acct-3 was never held")

	step("acct-4: twenty same-key messages in one range run in order on the fast path")
	waitUntil(func() bool { return len(entries("acct-4")) >= 20 }, 20*time.Second, "acct-4 to be entered twenty times")
	got = entries("acct-4")
	for i, seq := range got {
		if seq != i+1 {
			die(fmt.Sprintf("acct-4 handler order %v, want 1..20", got))
		}
	}
	waitForGone(ctx, ds, tp.Id, groupId, ids["acct-4/20"], 10*time.Second)
	if deferred := deferredLogRows(ctx, ds, tp.Id, groupId, "acct-4"); deferred != 0 {
		die(fmt.Sprintf("acct-4 wrote %d deferred log rows, want 0 -- the in-range chain should keep it off the exception path", deferred))
	}
	fmt.Println("PASS: 1..20 in order, no deferred rows")

	stop()
	if err := <-consumeDone; err != nil && !errors.Is(err, context.Canceled) {
		must(err)
	}

	fmt.Println("\n✅ ORDERED LAB PASSED")
	return nil
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

func waitUntil(condition func() bool, timeout time.Duration, what string) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	die(fmt.Sprintf("timed out waiting for %s", what))
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

func assertLogStatus(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64, attempt int, want string) {
	sql := fmt.Sprintf(`SELECT COALESCE(MAX(status), '') FROM %s.%s WHERE consumer_group_id = $1 AND message_id = $2 AND attempt = $3`, ds.Schema, topic.DeliveryLogTable(topicId))
	var status string
	must(ds.Pool.QueryRow(ctx, sql, groupId, messageId, attempt).Scan(&status))
	if status != want {
		die(fmt.Sprintf("message %d attempt %d log status %q, want %q", messageId, attempt, status, want))
	}
}

// deferredLogRows counts 'deferred' delivery_log rows for the key's messages.
func deferredLogRows(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, key string) int {
	sql := fmt.Sprintf(`
		SELECT COUNT(*)
		FROM %[1]s.%[2]s l JOIN %[1]s.%[3]s m ON m.id = l.message_id
		WHERE l.consumer_group_id = $1 AND m.message_key = $2 AND l.status = 'deferred'
	`, ds.Schema, topic.DeliveryLogTable(topicId), topic.MessageLogTable(topicId))
	var count int
	must(ds.Pool.QueryRow(ctx, sql, groupId, key).Scan(&count))
	return count
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
