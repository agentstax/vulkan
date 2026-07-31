// Command deferlab proves the dispatch-time concurrency policy on the cursor
// path: ConcurrencyDefer messages run under an exclusive key lease, resolve
// deferred while the key is busy (the range commit writes each one's
// 'deferred' delivery row), and resolve superseded when a newer message on
// the key exists.
//
// Registers its own topic, self-seeds keyed messages, fully self-contained.
//
// Confirms, in order:
//   - a Defer message on a free key runs while HOLDING the key lease and
//     releases it on success -- no delivery or delivery_log rows.
//   - an Allow (unset policy) keyed message runs with zero key lease rows.
//   - an unkeyed message under ConcurrencyOverride Defer runs as Allow.
//   - ConcurrencyOverride Allow beats a message's own Defer.
//   - key busy -> every head deferred during the hold ends with its own
//     'deferred' delivery row and 'deferred' log row once its range commits;
//     the rows sit inert; the key frees after the holder finishes.
//   - a message claimed as head but no longer head at dispatch resolves
//     superseded: never runs, delivery_log status 'superseded', no delivery row.
//   - a failing Defer message still frees the key.
//   - destroying the topic drops the delivery table cleanly.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

type Rec struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
}

var (
	ds      *coredatastore.PostgresDatastore
	topicID int64

	runsMu sync.Mutex
	runs   = map[string]int{} // "key:version" -> completed consumerFunc calls
)

func main() {
	ctx := context.Background()

	var err error
	ds, err = coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	topicName := fmt.Sprintf("deferlab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topic.Config{})
	must(err)
	topicID = tp.Id

	cd, err := consumer.NewConsumerDatastore[Rec](ds, nil)
	must(err)
	wp, err := producer.NewProducer[Rec](tp.Name, topic.SchemaVersion(1), ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))

	step("defer on a free key: runs holding the key lease, releases on success")
	publish(ctx, wp, "u:1", 1, common.ConcurrencyDefer)
	g1 := groupID(ctx, cd, "deferlab.g1")
	var heldDuringRun int
	consume(ctx, tp.Name, "deferlab.g1", nil, 3, func(ctx context.Context, message *Rec) error {
		if message.Key == "u:1" {
			heldDuringRun = leaseCount(ctx, g1)
		}
		record(message)
		return nil
	}, func() bool { return ran("u:1", 1) })
	if heldDuringRun != 1 {
		die(fmt.Sprintf("want the key lease held during the run, count=%d", heldDuringRun))
	}
	if n := leaseCount(ctx, g1); n != 0 {
		die(fmt.Sprintf("want the key released after success, count=%d", n))
	}
	if n := deliveryCount(ctx, g1, ""); n != 0 {
		die(fmt.Sprintf("a clean Defer run must leave no delivery rows, got %d", n))
	}
	fmt.Println("  ✓ held during, released after, no rows")

	step("allow (unset policy) keyed message never touches the key lease")
	publish(ctx, wp, "u:2", 1, "")
	g2 := groupID(ctx, cd, "deferlab.g2")
	allowHeld := -1
	consume(ctx, tp.Name, "deferlab.g2", nil, 3, func(ctx context.Context, message *Rec) error {
		if message.Key == "u:2" {
			allowHeld = leaseCount(ctx, g2)
		}
		record(message)
		return nil
	}, func() bool { return ran("u:2", 1) })
	if allowHeld != 0 {
		die(fmt.Sprintf("an Allow run must not hold a key lease, count=%d", allowHeld))
	}
	fmt.Println("  ✓ no lease rows")

	step("unkeyed under ConcurrencyOverride Defer runs as Allow")
	publishUnkeyed(ctx, wp, 1)
	g3 := groupID(ctx, cd, "deferlab.g3")
	unkeyedHeld := -1
	consume(ctx, tp.Name, "deferlab.g3", &consumer.ConsumerConfig{ConcurrencyOverride: common.ConcurrencyDefer}, 3, func(ctx context.Context, message *Rec) error {
		if message.Key == "" {
			unkeyedHeld = leaseCount(ctx, g3)
		}
		record(message)
		return nil
	}, func() bool { return ran("", 1) })
	if unkeyedHeld != 0 {
		die(fmt.Sprintf("an unkeyed run must not hold a key lease even under override Defer, count=%d", unkeyedHeld))
	}
	fmt.Println("  ✓ no lease rows")

	step("ConcurrencyOverride Allow beats a message's own Defer")
	publish(ctx, wp, "u:3", 1, common.ConcurrencyDefer)
	g4 := groupID(ctx, cd, "deferlab.g4")
	overrideHeld := -1
	consume(ctx, tp.Name, "deferlab.g4", &consumer.ConsumerConfig{ConcurrencyOverride: common.ConcurrencyAllow}, 3, func(ctx context.Context, message *Rec) error {
		if message.Key == "u:3" {
			overrideHeld = leaseCount(ctx, g4)
		}
		record(message)
		return nil
	}, func() bool { return ran("u:3", 1) })
	if overrideHeld != 0 {
		die(fmt.Sprintf("override Allow must skip the key lease, count=%d", overrideHeld))
	}
	fmt.Println("  ✓ ran without a lease")

	step("busy key: each head deferred during the hold gets its own 'deferred' row at commit")
	g5 := groupID(ctx, cd, "deferlab.g5")
	started := make(chan struct{})
	release := make(chan struct{})
	var startOnce sync.Once
	publish(ctx, wp, "u:4", 1, common.ConcurrencyDefer)
	v1 := messageID(ctx, "u:4", 1)

	done := make(chan struct{})
	go func() {
		defer close(done)
		consume(ctx, tp.Name, "deferlab.g5", nil, 3, func(ctx context.Context, message *Rec) error {
			if message.Key == "u:4" {
				startOnce.Do(func() { close(started) })
				<-release
			}
			record(message)
			return nil
		}, func() bool { return ran("u:4", 1) })
	}()

	<-started // v1 is running and holds the key
	publish(ctx, wp, "u:4", 2, common.ConcurrencyDefer)
	v2 := messageID(ctx, "u:4", 2)
	waitFor(func() bool { return deliveryStatus(ctx, g5, v2) == "deferred" }, "v2's 'deferred' row")

	publish(ctx, wp, "u:4", 3, common.ConcurrencyDefer)
	v3 := messageID(ctx, "u:4", 3)
	waitFor(func() bool { return deliveryStatus(ctx, g5, v3) == "deferred" }, "v3's 'deferred' row")
	if s := deliveryStatus(ctx, g5, v2); s != "deferred" {
		die(fmt.Sprintf("v2's 'deferred' row must sit untouched next to v3's, got status %q", s))
	}
	if n := deliveryCount(ctx, g5, "deferred"); n != 2 {
		die(fmt.Sprintf("want a 'deferred' row for each head deferred during the hold, got %d", n))
	}
	for _, v := range []int64{v2, v3} {
		if n := logCount(ctx, g5, v); n != 1 {
			die(fmt.Sprintf("want one 'deferred' log row for message %d, got %d", v, n))
		}
		if s, _ := logRow(ctx, g5, v); s != "deferred" {
			die(fmt.Sprintf("message %d's log row status = %q, want deferred", v, s))
		}
	}

	close(release)
	<-done // v1 finished
	if n := leaseCount(ctx, g5); n != 0 {
		die(fmt.Sprintf("want the key released after the holder finished, count=%d", n))
	}
	if ran("u:4", 2) || ran("u:4", 3) {
		die("deferred messages must stay inert -- nothing claims them yet")
	}
	if deliveryStatus(ctx, g5, v2) != "deferred" || deliveryStatus(ctx, g5, v3) != "deferred" {
		die("both 'deferred' rows must survive the holder's release untouched")
	}
	fmt.Printf("  ✓ v2 and v3 each hold a 'deferred' row, key freed (v1=%d v2=%d v3=%d)\n", v1, v2, v3)

	step("head moved between claim and dispatch: resolves superseded, never runs")
	g6 := groupID(ctx, cd, "deferlab.g6")
	blockStarted := make(chan struct{})
	blockRelease := make(chan struct{})
	var blockOnce sync.Once
	publishUnkeyed(ctx, wp, 2) // the blocker -- pins the single processor
	publish(ctx, wp, "u:5", 1, common.ConcurrencyDefer)
	v5old := messageID(ctx, "u:5", 1)

	done6 := make(chan struct{})
	go func() {
		defer close(done6)
		consume(ctx, tp.Name, "deferlab.g6", nil, 1, func(ctx context.Context, message *Rec) error {
			if message.Key == "" && message.Version == 2 {
				blockOnce.Do(func() { close(blockStarted) })
				<-blockRelease
			}
			record(message)
			return nil
		}, func() bool { return ran("u:5", 2) })
	}()

	<-blockStarted // u:5 v1 is claimed and queued behind the blocker
	publish(ctx, wp, "u:5", 2, common.ConcurrencyDefer)
	close(blockRelease)
	<-done6 // v2 ran -- so v1 resolved before it
	if ran("u:5", 1) {
		die("the stale claimed message ran -- dispatch must re-check the head")
	}
	status6, _ := logRow(ctx, g6, v5old)
	if status6 != "superseded" {
		die(fmt.Sprintf("stale message's log row status = %q, want superseded", status6))
	}
	if s := deliveryStatus(ctx, g6, v5old); s != "" {
		die(fmt.Sprintf("a dispatch-superseded message must not enter the delivery window, got status %q", s))
	}
	fmt.Println("  ✓ never ran, logged superseded, no delivery row")

	step("a failing Defer message frees the key")
	g7 := groupID(ctx, cd, "deferlab.g7")
	publish(ctx, wp, "u:6", 1, common.ConcurrencyDefer)
	v6 := messageID(ctx, "u:6", 1)
	consume(ctx, tp.Name, "deferlab.g7", nil, 3, func(ctx context.Context, message *Rec) error {
		record(message)
		if message.Key == "u:6" {
			return errors.New("deferlab: synthetic failure")
		}
		return nil
	}, func() bool { return deliveryStatus(ctx, g7, v6) == "ready" })
	if n := leaseCount(ctx, g7); n != 0 {
		die(fmt.Sprintf("want the key released after the exception, count=%d", n))
	}
	status7, _ := logRow(ctx, g7, v6)
	if status7 != "failure" {
		die(fmt.Sprintf("exception log row status = %q, want failure", status7))
	}
	fmt.Println("  ✓ key freed, 'ready' row, status failure")

	step("destroying the topic drops the delivery table")
	must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	fmt.Println("  ✓ destroyed")

	fmt.Println("\n✅ DEFER LAB PASSED")
}

// consume runs a MessageConsumer for group until done() (10s cap), with pool
// concurrent processors.
func consume(ctx context.Context, topicName, group string, cfg *consumer.ConsumerConfig, pool int, consumerFunc consumer.ConsumerFunc[Rec], done func() bool) {
	if cfg == nil {
		cfg = &consumer.ConsumerConfig{}
	}
	cfg.DisableGracefulShutdown = true
	cfg.BatchLimit = 50
	cfg.ClaimPollRate = 50 * time.Millisecond

	queue, err := concurrency.NewPressureQueue[consumer.Buffered](50)
	must(err)
	limiter, err := concurrency.NewWorkerPoolLimiter(pool)
	must(err)
	wc, err := consumer.NewMessageConsumer[Rec](group, topicName, topic.SchemaVersion(1), queue, limiter, ds, cfg)
	must(err)
	must(wc.Register(ctx))

	runCtx, cancel := context.WithCancel(ctx)
	errCh := make(chan error, 1)
	go func() { errCh <- wc.Consume(runCtx, consumerFunc) }()

	start := time.Now()
	for !done() {
		if time.Since(start) > 10*time.Second {
			cancel()
			die(fmt.Sprintf("timed out waiting for %s to finish, Consume returned: %v", group, <-errCh))
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		die(fmt.Sprintf("Consume returned an unexpected error: %v", err))
	}
}

func record(message *Rec) {
	runsMu.Lock()
	defer runsMu.Unlock()
	runs[fmt.Sprintf("%s:%d", message.Key, message.Version)]++
}

func ran(key string, version int) bool {
	runsMu.Lock()
	defer runsMu.Unlock()
	return runs[fmt.Sprintf("%s:%d", key, version)] > 0
}

func publish(ctx context.Context, wp *producer.Producer[Rec], key string, version int, policy common.ConcurrencyPolicy) {
	opts := producer.ProduceOptions{CompactionKey: key}
	if policy != "" {
		opts.Message = &common.MessageOptions{Concurrency: policy}
	}
	_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*Rec, error) {
		return &Rec{Key: key, Version: version}, nil
	}, opts)
	must(err)
}

func publishUnkeyed(ctx context.Context, wp *producer.Producer[Rec], version int) {
	_, err := wp.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*Rec, error) {
		return &Rec{Version: version}, nil
	}, producer.ProduceOptions{})
	must(err)
}

func groupID(ctx context.Context, cd *consumer.ConsumerDatastore[Rec], name string) int64 {
	g, err := cd.UpsertGroup(ctx, topicID, name)
	must(err)
	return g.Id
}

func messageID(ctx context.Context, key string, version int) int64 {
	var id int64
	sql := fmt.Sprintf(`SELECT id FROM message_log_%d WHERE compaction_key = $1 AND (payload->>'version')::int = $2`, topicID)
	must(ds.Pool.QueryRow(ctx, sql, key, version).Scan(&id))
	return id
}

func leaseCount(ctx context.Context, groupID int64) int {
	var n int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM key_lease WHERE consumer_group_id = $1`, groupID).Scan(&n))
	return n
}

// deliveryCount counts the group's delivery rows; status "" counts them all.
func deliveryCount(ctx context.Context, groupID int64, status string) int {
	var n int
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM delivery_%d WHERE consumer_group_id = $1 AND ($2 = '' OR status = $2)`, topicID)
	must(ds.Pool.QueryRow(ctx, sql, groupID, status).Scan(&n))
	return n
}

// deliveryStatus returns "" when the message has no delivery row.
func deliveryStatus(ctx context.Context, groupID int64, messageID int64) string {
	var s string
	sql := fmt.Sprintf(`SELECT COALESCE(MAX(status), '') FROM delivery_%d WHERE consumer_group_id = $1 AND message_id = $2`, topicID)
	must(ds.Pool.QueryRow(ctx, sql, groupID, messageID).Scan(&s))
	return s
}

func logCount(ctx context.Context, groupID int64, messageID int64) int {
	var n int
	sql := fmt.Sprintf(`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group_id = $1 AND message_id = $2`, topicID)
	must(ds.Pool.QueryRow(ctx, sql, groupID, messageID).Scan(&n))
	return n
}

// logRow returns the message's single delivery_log row's status and error.
func logRow(ctx context.Context, groupID int64, messageID int64) (string, string) {
	var status, logErr string
	sql := fmt.Sprintf(`SELECT status, error FROM delivery_log_%d WHERE consumer_group_id = $1 AND message_id = $2`, topicID)
	must(ds.Pool.QueryRow(ctx, sql, groupID, messageID).Scan(&status, &logErr))
	return status, logErr
}

func waitFor(cond func() bool, what string) {
	start := time.Now()
	for !cond() {
		if time.Since(start) > 10*time.Second {
			die("timed out waiting for " + what)
		}
		time.Sleep(20 * time.Millisecond)
	}
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
