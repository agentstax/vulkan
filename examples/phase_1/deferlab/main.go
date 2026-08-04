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
//   - redemption: a stale 'deferred' row resolves superseded with its log row
//     and never runs; the head's row runs and pops.
//   - a row whose compaction key has an unexpired key_lease is never claimed:
//     no attempts motion, no log rows; the kill backstop never touches a
//     'deferred' row; the run lands once the key frees.
//   - a failed holder's retry passes the key gate and resolves superseded once
//     a newer head exists, its claim-time attempts increment decremented back.
//   - a crashed holder's expired key lease: redemption takes the key over.
//   - Concurrency Defer without a CompactionKey is refused at produce time.
//   - torture: two cursor consumers and two exception consumers fight one key
//     through an abandoned (past-Timeout) holder and head churn -- only the
//     final head ever runs, exactly once, every other version audits out
//     'superseded'.
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
	"github.com/agentstax/vulkan/pkg/consumer"
	consumermetrics "github.com/agentstax/vulkan/pkg/consumer/metrics"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/agentstax/vulkan/pkg/worker"
	workercontroller "github.com/agentstax/vulkan/pkg/worker/controller"
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
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
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
		die("deferred messages must stay inert until an ExceptionConsumer runs")
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

	step("redemption: the stale 'deferred' row resolves superseded, the head runs and pops")
	stopRedeem := startExceptionConsumer(ctx, tp.Name, "deferlab.g5", nil, func(ctx context.Context, message *Rec) error {
		record(message)
		return nil
	})
	// ran() flips inside consumerFunc, before the outcome lands -- wait on the
	// recorded state, not the run
	waitFor(func() bool {
		return ran("u:4", 3) && deliveryStatus(ctx, g5, v2) == "superseded" && deliveryStatus(ctx, g5, v3) == ""
	}, "v3 to run and pop, v2 to resolve superseded")
	stopRedeem()
	if ran("u:4", 2) {
		die("a stale 'deferred' message must never run")
	}
	if n := deliveryAttempts(ctx, g5, v2); n != 0 {
		die(fmt.Sprintf("a superseded 'deferred' row never ran, attempts must net 0, got %d", n))
	}
	// v2's audit trail: 'deferred' at attempt 0 from its range commit,
	// 'superseded' at attempt 1 from redemption
	statuses := logStatuses(ctx, g5, v2)
	if len(statuses) != 2 || statuses[0] != "deferred" || statuses[1] != "superseded" {
		die(fmt.Sprintf("v2's log rows = %v, want deferred at 0 and superseded at 1", statuses))
	}
	if n := logCount(ctx, g5, v3); n != 1 {
		die(fmt.Sprintf("a redeemed success leaves only its commit-time 'deferred' log row, got %d rows", n))
	}
	if n := leaseCount(ctx, g5); n != 0 {
		die(fmt.Sprintf("want the key released after redemption, count=%d", n))
	}
	fmt.Println("  ✓ v2 superseded with full audit, v3 ran and popped")

	step("a held key excludes its 'deferred' row from the claim, kill backstop blind to it")
	g8 := groupID(ctx, cd, "deferlab.g8")
	started8 := make(chan struct{})
	release8 := make(chan struct{})
	var once8 sync.Once
	stopCursor8 := startConsumer(ctx, tp.Name, "deferlab.g8", nil, 3, func(ctx context.Context, message *Rec) error {
		if message.Key == "u:7" && message.Version == 1 {
			once8.Do(func() { close(started8) })
			<-release8
		}
		record(message)
		return nil
	})
	publish(ctx, wp, "u:7", 1, common.ConcurrencyDefer)
	<-started8 // v1 is running and holds the key
	publish(ctx, wp, "u:7", 2, common.ConcurrencyDefer)
	v7 := messageID(ctx, "u:7", 2)
	waitFor(func() bool { return deliveryStatus(ctx, g8, v7) == "deferred" }, "v2's 'deferred' row")

	// exhausted-looking or not, a 'deferred' row is outside the kill
	// backstop's 'inflight' predicate. Driven directly so no consumer touches
	// the row mid-check.
	execSql(ctx, fmt.Sprintf(`UPDATE delivery_%d SET attempts = 99, lease_until = now() - interval '1 minute' WHERE consumer_group_id = $1 AND message_id = $2`, topicID), g8, v7)
	must(cd.KillExceptions(ctx, tp.Id, g8, 3, false))
	if s := deliveryStatus(ctx, g8, v7); s != "deferred" {
		die(fmt.Sprintf("the kill backstop must never touch a 'deferred' row, got status %q", s))
	}
	if _, err := cd.ClaimExceptions(ctx, tp.Id, g8, 10, 3, 5*time.Second, false); err != nil {
		die(fmt.Sprintf("ClaimExceptions: %v", err))
	}
	if n := deliveryAttempts(ctx, g8, v7); n != 99 {
		die(fmt.Sprintf("an exhausted row must not be claimed, attempts = %d", n))
	}
	// the unexpired key_lease alone must exclude the row -- attempts back at
	// 0, well under the ceiling
	execSql(ctx, fmt.Sprintf(`UPDATE delivery_%d SET attempts = 0 WHERE consumer_group_id = $1 AND message_id = $2`, topicID), g8, v7)
	if _, err := cd.ClaimExceptions(ctx, tp.Id, g8, 10, 3, 5*time.Second, false); err != nil {
		die(fmt.Sprintf("ClaimExceptions: %v", err))
	}
	if s, n := deliveryStatus(ctx, g8, v7), deliveryAttempts(ctx, g8, v7); s != "deferred" || n != 0 {
		die(fmt.Sprintf("a row whose compaction key has an unexpired key_lease must not be claimed, got status %q attempts %d", s, n))
	}

	stopRedeem8 := startExceptionConsumer(ctx, tp.Name, "deferlab.g8", nil, func(ctx context.Context, message *Rec) error {
		record(message)
		return nil
	})
	time.Sleep(300 * time.Millisecond) // several claim polls against the held key
	if ran("u:7", 2) {
		die("nothing may run while another delivery holds the key")
	}
	// the row is never claimed while the key is held -- status and attempts
	// hold still, so read them directly, no sampling
	if s, n := deliveryStatus(ctx, g8, v7), deliveryAttempts(ctx, g8, v7); s != "deferred" || n != 0 {
		die(fmt.Sprintf("live claim polls must leave the excluded row untouched, got status %q attempts %d", s, n))
	}
	if n := logCount(ctx, g8, v7); n != 1 {
		die(fmt.Sprintf("an unclaimed row must not grow log rows, got %d", n))
	}

	close(release8)
	waitFor(func() bool { return ran("u:7", 2) }, "v2 to run once the key freed")
	waitFor(func() bool { return deliveryStatus(ctx, g8, v7) == "" && leaseCount(ctx, g8) == 0 }, "v2's row to pop and the key to free")
	stopCursor8()
	stopRedeem8()
	fmt.Println("  ✓ excluded from the claim while held, survived the backstop, ran on release")

	step("a failed holder's retry supersedes once a newer head runs")
	g9 := groupID(ctx, cd, "deferlab.g9")
	failV1 := func(ctx context.Context, message *Rec) error {
		record(message)
		if message.Key == "u:8" && message.Version == 1 {
			return errors.New("deferlab: synthetic holder failure")
		}
		return nil
	}
	stopCursor9 := startConsumer(ctx, tp.Name, "deferlab.g9", nil, 3, failV1)
	stopRedeem9 := startExceptionConsumer(ctx, tp.Name, "deferlab.g9", nil, failV1)
	publish(ctx, wp, "u:8", 1, common.ConcurrencyDefer)
	v8old := messageID(ctx, "u:8", 1)
	waitFor(func() bool { return deliveryStatus(ctx, g9, v8old) == "ready" }, "v1 to fail and go 'ready'")
	publish(ctx, wp, "u:8", 2, common.ConcurrencyDefer)
	waitFor(func() bool { return ran("u:8", 2) }, "v2 to run on the freed key")
	waitFor(func() bool { return deliveryStatus(ctx, g9, v8old) == "superseded" }, "v1's retry to resolve superseded")
	stopCursor9()
	stopRedeem9()
	// the superseded log row lands one above the last counted attempt --
	// RecordExceptionSuperseded decremented the refused claim's increment back
	statuses9 := logStatuses(ctx, g9, v8old)
	sup := -1
	for attempt, s := range statuses9 {
		if s == "superseded" {
			sup = attempt
		}
	}
	if sup == -1 {
		die(fmt.Sprintf("v1's log rows = %v, want a superseded row", statuses9))
	}
	if att := deliveryAttempts(ctx, g9, v8old); sup != att+1 {
		die(fmt.Sprintf("superseded logged at attempt %d with attempts %d, want attempts + 1", sup, att))
	}
	if n := leaseCount(ctx, g9); n != 0 {
		die(fmt.Sprintf("want the key released, count=%d", n))
	}
	fmt.Println("  ✓ retry gate refused v1, attempts decremented back, superseded logged")

	step("a crashed holder's expired key lease: redemption takes the key over")
	g10 := groupID(ctx, cd, "deferlab.g10")
	// a crashed holder's key_lease row: unexpired, never released
	execSql(ctx, `INSERT INTO key_lease (consumer_group_id, compaction_key, lease_token, expires_at) VALUES ($1, 'u:10', gen_random_uuid(), now() + interval '1500 milliseconds')`, g10)
	publish(ctx, wp, "u:10", 1, common.ConcurrencyDefer)
	v10 := messageID(ctx, "u:10", 1)
	stopCursor10 := startConsumer(ctx, tp.Name, "deferlab.g10", nil, 3, func(ctx context.Context, message *Rec) error {
		record(message)
		return nil
	})
	stopRedeem10 := startExceptionConsumer(ctx, tp.Name, "deferlab.g10", nil, func(ctx context.Context, message *Rec) error {
		record(message)
		return nil
	})
	waitFor(func() bool { return deliveryStatus(ctx, g10, v10) == "deferred" }, "v1 to defer behind the crashed holder's key")
	if ran("u:10", 1) {
		die("nothing may run while the crashed holder's lease is live")
	}
	waitFor(func() bool { return ran("u:10", 1) }, "redemption to take the expired key over")
	waitFor(func() bool { return deliveryStatus(ctx, g10, v10) == "" && leaseCount(ctx, g10) == 0 }, "the row to pop and the key to free")
	stopCursor10()
	stopRedeem10()
	fmt.Println("  ✓ deferred behind the crashed holder, ran after expiry via takeover")

	step("Defer without a CompactionKey is refused at produce time")
	if _, err := wp.Produce(ctx, &Rec{Version: 1}, producer.ProduceOptions{Message: &common.MessageOptions{Concurrency: common.ConcurrencyDefer}}); err == nil {
		die("produce must refuse Defer without a CompactionKey")
	}
	fmt.Println("  ✓ refused")

	step("torture: two consumers per loop fight one key through an abandoned holder and head churn")
	g11 := groupID(ctx, cd, "deferlab.g11")
	// a short per-message Timeout so v1's sleeping run is abandoned mid-hold --
	// its failure recording frees the key while the goroutine sleeps on
	tortureCfg := func() *consumer.ConsumerConfig {
		return &consumer.ConsumerConfig{Message: &common.MessageOptions{Timeout: 500 * time.Millisecond}}
	}
	started11 := make(chan struct{})
	var once11 sync.Once
	tortureFunc := func(ctx context.Context, message *Rec) error {
		record(message)
		if message.Key == "u:11" && message.Version == 1 {
			once11.Do(func() { close(started11) })
			time.Sleep(2 * time.Second) // ignores ctx -- abandoned at Timeout
		}
		return nil
	}
	stopCursor11a := startConsumer(ctx, tp.Name, "deferlab.g11", tortureCfg(), 3, tortureFunc)
	stopCursor11b := startConsumer(ctx, tp.Name, "deferlab.g11", tortureCfg(), 3, tortureFunc)
	stopRedeem11a := startExceptionConsumer(ctx, tp.Name, "deferlab.g11", tortureCfg(), tortureFunc)
	stopRedeem11b := startExceptionConsumer(ctx, tp.Name, "deferlab.g11", tortureCfg(), tortureFunc)

	publish(ctx, wp, "u:11", 1, common.ConcurrencyDefer)
	tv1 := messageID(ctx, "u:11", 1)
	<-started11 // v1 runs holding the key
	publish(ctx, wp, "u:11", 2, common.ConcurrencyDefer)
	publish(ctx, wp, "u:11", 3, common.ConcurrencyDefer)
	publish(ctx, wp, "u:11", 4, common.ConcurrencyDefer)
	tv2 := messageID(ctx, "u:11", 2)
	tv3 := messageID(ctx, "u:11", 3)
	tv4 := messageID(ctx, "u:11", 4)

	// interleaving-robust: whichever versions deferred vs superseded at
	// dispatch, the end state is fixed -- v4 runs and pops, everything else
	// resolves without running, nothing is left unresolved
	waitFor(func() bool {
		return runCount("u:11", 4) == 1 &&
			deliveryStatus(ctx, g11, tv4) == "" &&
			deliveryStatus(ctx, g11, tv1) == "superseded" &&
			deliveryCount(ctx, g11, "ready") == 0 &&
			deliveryCount(ctx, g11, "inflight") == 0 &&
			deliveryCount(ctx, g11, "deferred") == 0 &&
			leaseCount(ctx, g11) == 0
	}, "v4 to run once, v1's retry to resolve superseded, every row to resolve")
	stopCursor11a()
	stopCursor11b()
	stopRedeem11a()
	stopRedeem11b()

	if n := runCount("u:11", 1); n != 1 {
		die(fmt.Sprintf("the abandoned holder must have run exactly once, ran %d times", n))
	}
	if n := runCount("u:11", 4); n != 1 {
		die(fmt.Sprintf("the final head must run exactly once across four racing consumers, ran %d times", n))
	}
	// a non-head ends one of three ways: claim-time compacted (no rows at
	// all -- the documented silent drop), dispatch-superseded (log row only),
	// or 'deferred' then redemption-superseded (delivery row + log row)
	for version, id := range map[int]int64{2: tv2, 3: tv3} {
		if ran("u:11", version) {
			die(fmt.Sprintf("non-head v%d must never run", version))
		}
		status := deliveryStatus(ctx, g11, id)
		if status != "superseded" && status != "" {
			die(fmt.Sprintf("v%d must end superseded or dropped, got status %q", version, status))
		}
		sup := 0
		for _, s := range logStatuses(ctx, g11, id) {
			if s == "superseded" {
				sup++
			}
		}
		if sup > 1 {
			die(fmt.Sprintf("v%d must never audit more than one 'superseded' log row, got %d", version, sup))
		}
		if status == "superseded" && sup != 1 {
			die(fmt.Sprintf("v%d's 'superseded' delivery row needs its log row, got %d", version, sup))
		}
	}
	fmt.Printf("  ✓ v4 ran once, v1-v3 audited out superseded (v1=%d v2=%d v3=%d v4=%d)\n", tv1, tv2, tv3, tv4)

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

	cfg.QueueSize = 50
	cfg.MessageConcurrency = pool
	cfg.WithDefaults()

	owner := groupOwner(ctx, topicName, group)
	definition, err := consumer.NewMessageConsumerDefinition(ds, consumerFunc, abandonedEventProducer(ctx), cfg)
	must(err)

	runCtx, cancel := context.WithCancel(ctx)
	execution := claimOne(runCtx, definition, owner)
	errCh := make(chan error, 1)
	go func() { errCh <- execution.Run(runCtx) }()

	start := time.Now()
	for !done() {
		if time.Since(start) > 10*time.Second {
			cancel()
			die(fmt.Sprintf("timed out waiting for %s to finish, Run returned: %v", group, <-errCh))
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
		die(fmt.Sprintf("Run returned an unexpected error: %v", err))
	}
}

// startConsumer runs a MessageConsumer for group until the returned stop is
// called. cfg may be nil.
func startConsumer(ctx context.Context, topicName, group string, cfg *consumer.ConsumerConfig, pool int, consumerFunc consumer.ConsumerFunc[Rec]) func() {
	if cfg == nil {
		cfg = &consumer.ConsumerConfig{}
	}
	cfg.DisableGracefulShutdown = true
	cfg.BatchLimit = 50
	cfg.ClaimPollRate = 50 * time.Millisecond
	cfg.QueueSize = 50
	cfg.MessageConcurrency = pool
	cfg.WithDefaults()

	owner := groupOwner(ctx, topicName, group)
	definition, err := consumer.NewMessageConsumerDefinition(ds, consumerFunc, abandonedEventProducer(ctx), cfg)
	must(err)

	runCtx, cancel := context.WithCancel(ctx)
	execution := claimOne(runCtx, definition, owner)
	errCh := make(chan error, 1)
	go func() { errCh <- execution.Run(runCtx) }()
	return func() {
		cancel()
		if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
			die(fmt.Sprintf("Run returned an unexpected error: %v", err))
		}
	}
}

// startExceptionConsumer runs an ExceptionConsumer (exception retries +
// deferred redemption, one claim) for group until the returned stop is
// called. cfg may be nil.
func startExceptionConsumer(ctx context.Context, topicName, group string, cfg *consumer.ConsumerConfig, consumerFunc consumer.ConsumerFunc[Rec]) func() {
	if cfg == nil {
		cfg = &consumer.ConsumerConfig{}
	}
	cfg.DisableGracefulShutdown = true
	cfg.BatchLimit = 50
	cfg.ClaimPollRate = 50 * time.Millisecond
	cfg.ExceptionInitialBackoff = 50 * time.Millisecond
	cfg.WithDefaults()

	owner := groupOwner(ctx, topicName, group)
	definition, err := consumer.NewExceptionConsumerDefinition(ds, consumerFunc, abandonedEventProducer(ctx), cfg)
	must(err)

	runCtx, cancel := context.WithCancel(ctx)
	execution := claimOne(runCtx, definition, owner)
	errCh := make(chan error, 1)
	go func() { errCh <- execution.Run(runCtx) }()
	return func() {
		cancel()
		if err := <-errCh; err != nil && !errors.Is(err, context.Canceled) {
			die(fmt.Sprintf("exception Run returned an unexpected error: %v", err))
		}
	}
}

func groupOwner(ctx context.Context, topicName string, group string) *common.Owner {
	topicController, err := topiccontroller.NewTopicController(ds, nil)
	must(err)
	tp, err := topicController.GetTopic(ctx, topicName, topic.SchemaVersion(1))
	must(err)

	consumerDatastore, err := consumer.NewConsumerDatastore[Rec](ds, nil)
	must(err)
	g, err := consumerDatastore.RegisterGroup(ctx, tp.Id, group)
	must(err)

	owner, err := common.NewConsumerGroupOwner(tp.SystemId, tp.Id, g.Id, g.Name)
	must(err)
	return owner
}

func abandonedEventProducer(ctx context.Context) *consumermetrics.MetricEventProducer {
	events, err := consumermetrics.NewMetricEventProducer(ds, &consumermetrics.MetricEventConfig{DisableGracefulShutdown: true})
	must(err)
	must(events.Register(ctx))
	return events
}

// no manager, so nothing respawns the execution -- the lab decides exactly how
// many run
func claimOne(ctx context.Context, definition worker.Definition, owner *common.Owner) worker.Execution {
	must(definition.Declare(ctx, owner))

	workers, err := workercontroller.NewWorkerController(ds, nil)
	must(err)
	row, err := workers.GetWorker(ctx, definition.Name(), owner)
	must(err)

	execution, err := definition.Provision(ctx, row.Id, &row.Owner, row.Metadata)
	must(err)
	return execution
}

func record(message *Rec) {
	runsMu.Lock()
	defer runsMu.Unlock()
	runs[fmt.Sprintf("%s:%d", message.Key, message.Version)]++
}

func runCount(key string, version int) int {
	runsMu.Lock()
	defer runsMu.Unlock()
	return runs[fmt.Sprintf("%s:%d", key, version)]
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
	g, err := cd.RegisterGroup(ctx, topicID, name)
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

func deliveryAttempts(ctx context.Context, groupID int64, messageID int64) int {
	var n int
	sql := fmt.Sprintf(`SELECT attempts FROM delivery_%d WHERE consumer_group_id = $1 AND message_id = $2`, topicID)
	must(ds.Pool.QueryRow(ctx, sql, groupID, messageID).Scan(&n))
	return n
}

// logStatuses returns the message's delivery_log statuses keyed by attempt.
func logStatuses(ctx context.Context, groupID int64, messageID int64) map[int]string {
	sql := fmt.Sprintf(`SELECT attempt, status FROM delivery_log_%d WHERE consumer_group_id = $1 AND message_id = $2`, topicID)
	rows, err := ds.Pool.Query(ctx, sql, groupID, messageID)
	must(err)
	defer rows.Close()

	statuses := map[int]string{}
	for rows.Next() {
		var attempt int
		var status string
		must(rows.Scan(&attempt, &status))
		statuses[attempt] = status
	}
	must(rows.Err())
	return statuses
}

func execSql(ctx context.Context, sql string, args ...any) {
	_, err := ds.Pool.Exec(ctx, sql, args...)
	must(err)
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
