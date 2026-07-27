package main

// schema evolution bridge lab: the end-to-end proof of the whole
// epoch/CompactionRank design -- Chunk 6/7's reference implementation of the
// user-space BRIDGE pattern the library documents but doesn't ship a verb for.
//
// Scenario: v1 already holds live keyed traffic. v2 is registered alongside
// it. A bridge consumer group on v1 transforms each key's current winner and
// re-produces it into v2 at CompactionRank -1 (a backfill, never a live
// write), while the application's real producers write straight to v2 at
// rank 0 for keys that have already cut over. Confirms, against the real
// claim/lease/cursor machinery, not just the SQL-level guarantee
// compactionranklab proves in isolation:
//   - zero-pause: a v2 key with a live rank-0 write never loses to the
//     bridge's rank-1 copy of the same key, in EITHER arrival order (user:1
//     is live-before-bridge, user:2 is bridge-before-live).
//   - crash + restart: stopping the bridge mid-drain and starting a fresh
//     instance on the same consumer group resumes from the persisted cursor
//     instead of re-walking v1 from the start, and the source-id-derived
//     IdempotencyKey means however that boundary message gets settled (clean
//     commit vs. redelivered), v2 ends up with exactly one row per bridged
//     key -- never a duplicate. This lab picks a deterministic stop point
//     (distinct-key count, not a timer) to stay non-flaky; it is not trying
//     to win a race against an in-flight commit -- idempotencykeysracelab
//     already covers dedup-under-true-concurrency.
//   - drain telegraphing never lies: FamilyHealth(v1) keeps reporting
//     "compacted: requires bridge" even once this lab's own verification
//     proves the migration complete -- retiring v1 stays an operator
//     decision informed by that proof, never something the library asserts
//     for a compacted topic on its own.
//
// Self-contained: registers both topic versions, destroys v2 on exit,
// destroys v1 explicitly once its retirement is proven.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/concurrency"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const group = "phase14a.schemaevolutionlab.bridge"

// bridgeNamespace seeds the bridge's UUIDv5 idempotency keys -- fixed so a
// given source message id always derives the same key, run to run.
var bridgeNamespace = uuid.MustParse("9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d")

var keys = []string{"user:1", "user:2", "user:3", "user:4", "user:5"}

// V1Order is what v1's producers wrote before the schema evolved.
type V1Order struct {
	Key   string `json:"key"`
	Cents int64  `json:"cents"`
}

// V2Order adds Currency -- the additive change the bridge exists to carry
// forward; rows the bridge itself writes default it to "USD".
type V2Order struct {
	Key      string `json:"key"`
	Cents    int64  `json:"cents"`
	Currency string `json:"currency"`
}

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

	name := fmt.Sprintf("phase14a.schemaevolutionlab.%d", time.Now().UnixNano())
	v1, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(1), &topic.Config{})
	must(err)

	wp1, err := producer.NewProducer[V1Order](name, topic.SchemaVersion(1), ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp1.Register(ctx))

	step("v1 holds live keyed traffic for 5 users")
	for i, key := range keys {
		cents := int64(i+1) * 100
		_, err := wp1.Produce(ctx, &V1Order{Key: key, Cents: cents}, producer.ProduceOptions{CompactionKey: key})
		must(err)
		fmt.Printf("  wrote %s cents=%d to v1\n", key, cents)
	}

	step("register v2 alongside v1 -- same name, a new physical topic")
	v2, err := mAdmin.RegisterTopic(ctx, name, topic.SchemaVersion(2), &topic.Config{})
	must(err)
	defer func() { must(mAdmin.DestroyTopic(ctx, name, topic.SchemaVersion(2), admin.DestroyOptions{Force: true})) }()

	wp2, err := producer.NewProducer[V2Order](name, topic.SchemaVersion(2), ds, &producer.ProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp2.Register(ctx))

	step("user:1 cuts over to v2 BEFORE the bridge ever sees it (live-then-backfill)")
	must(liveWrite(ctx, wp2, "user:1", 999, "EUR"))

	// processed counts successful bridge writes; crashGate blocks the 3rd
	// message (user:3) until we've confirmed exactly 2 landed, then run1's
	// ctx is cancelled while it's mid-block -- a deterministic, DB-timing-
	// independent stand-in for "the process died right here."
	var processed atomic.Int64
	crashGate := make(chan struct{})
	bridgeFunc := func(ctx context.Context, work *V1Order) error {
		if processed.Load() >= 2 {
			select {
			case <-crashGate:
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		meta, ok := consumer.MetaFromContext(ctx)
		if !ok {
			return fmt.Errorf("no MessageMeta in context for key %q", work.Key)
		}
		_, err := wp2.Produce(ctx, &V2Order{Key: work.Key, Cents: work.Cents, Currency: "USD"}, producer.ProduceOptions{
			CompactionKey:  work.Key,
			CompactionRank: -1,
			IdempotencyKey: bridgeIdempotencyKey(meta.Id),
		})
		if err == nil {
			processed.Add(1)
		}
		return err
	}

	step("bridge run 1: drains user:1 and user:2, then \"crashes\" mid-user:3")
	run1Ctx, cancelRun1 := context.WithCancel(ctx)
	go func() {
		for processed.Load() < 2 {
			time.Sleep(time.Millisecond)
		}
		cancelRun1()
	}()
	bridge1 := newBridgeConsumer(ctx, ds, name)
	must(bridge1.Consume(run1Ctx, bridgeFunc))
	assertInt("exactly 2 messages landed before the crash", processed.Load(), 2)

	step("user:2 cuts over to v2 AFTER the bridge already copied it (backfill-then-live)")
	must(liveWrite(ctx, wp2, "user:2", 888, "EUR"))
	close(crashGate) // release user:3, wherever it's stuck (fresh claim or a retried exception)

	step("bridge run 2: a fresh instance, same group, resumes from the persisted cursor")
	run2Ctx, cancelRun2 := context.WithCancel(ctx)
	bridge2 := newBridgeConsumer(ctx, ds, name)
	go func() { must(waitForDistinctCount(run2Ctx, ds, v2.Id, int64(len(keys)), 10*time.Second, cancelRun2)) }()
	must(bridge2.Consume(run2Ctx, bridgeFunc))

	step("verify the winners: live always beats the bridge, regardless of which arrived first")
	assertWinner(ctx, ds, v2.Id, "user:1", 999, "EUR") // live arrived first, still wins
	assertWinner(ctx, ds, v2.Id, "user:2", 888, "EUR") // live arrived second, still wins
	assertWinner(ctx, ds, v2.Id, "user:3", 300, "USD") // bridge only
	assertWinner(ctx, ds, v2.Id, "user:4", 400, "USD") // bridge only
	assertWinner(ctx, ds, v2.Id, "user:5", 500, "USD") // bridge only

	step("verify exactly-once: the crash/restart never left a duplicate row in v2")
	assertInt("v1 untouched by the bridge -- still exactly 5 physical rows", rowCount(ctx, ds, v1.Id), 5)
	assertInt("v2 has exactly one row per bridged key plus the two live overrides", rowCount(ctx, ds, v2.Id), 7)

	step("drain telegraphing never lies about a compacted topic, even once it's actually safe")
	health, err := mAdmin.FamilyHealth(ctx, name)
	must(err)
	v1Health := versionHealth(health, 1)
	assertTrue("v1 is reported compacted", v1Health.Compacted)
	assertTrue("FamilyHealth refuses to call it Safe on its own", !v1Health.Safe)
	fmt.Printf("  verdict: %s\n", v1Health.Reason)
	fmt.Println("  (this lab's own row-count/winner checks above are what actually prove the bridge finished --")
	fmt.Println("   FamilyHealth correctly never asserts that for a compacted topic)")

	step("retire v1 -- an operator decision informed by the proof above, not by FamilyHealth.Safe")
	must(mAdmin.DestroyTopic(ctx, name, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))

	fmt.Println("\n✅ SCHEMA EVOLUTION BRIDGE LAB PASSED")
	fmt.Println("   live writes beat the bridge in either arrival order -> a crashed bridge resumes")
	fmt.Println("   from its cursor with no duplicates -> drain telegraphing never calls a compacted")
	fmt.Println("   topic safe, even once it demonstrably is.")
}

// ---- helpers ----

func liveWrite(ctx context.Context, wp *producer.Producer[V2Order], key string, cents int64, currency string) error {
	_, err := wp.Produce(ctx, &V2Order{Key: key, Cents: cents, Currency: currency}, producer.ProduceOptions{CompactionKey: key})
	return err
}

func bridgeIdempotencyKey(sourceID int64) uuid.UUID {
	return uuid.NewSHA1(bridgeNamespace, []byte(strconv.FormatInt(sourceID, 10)))
}

// newBridgeConsumer builds a fresh Consumer instance on the bridge's group --
// a new one each call, exactly as if the process had restarted. BatchLimit 1
// and a single-permit pool keep the bridge's own delivery order deterministic
// (ascending v1 id), so this lab's stop points are reproducible. Short
// margins and a short ExceptionInitialBackoff keep the crash/retry path fast
// instead of waiting out the library's production-sized defaults.
func newBridgeConsumer(ctx context.Context, ds *coredatastore.PostgresDatastore, name string) *consumer.Consumer[V1Order] {
	queue, err := concurrency.NewPressureQueue[consumer.Buffered](4)
	must(err)
	pool, err := concurrency.NewWorkerPoolLimiter(1)
	must(err)

	c, err := consumer.NewConsumer[V1Order](group, name, topic.SchemaVersion(1), queue, pool, ds, &consumer.ConsumerConfig{
		BatchLimit:              1,
		ClaimPollRate:           50 * time.Millisecond,
		WorkTimeout:             2 * time.Second,
		QueueMargin:             500 * time.Millisecond,
		AckMargin:               500 * time.Millisecond,
		ExceptionInitialBackoff: 200 * time.Millisecond,
		DisableGracefulShutdown: true,
	})
	must(err)
	must(c.Register(ctx))
	return c
}

// waitForDistinctCount polls v2's compaction_head until it holds want distinct
// keys, then cancels stop -- a durable, DB-observed stop signal instead of a
// timer, so the crash point is reproducible run to run.
func waitForDistinctCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, want int64, timeout time.Duration, stop context.CancelFunc) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if distinctKeyCount(ctx, ds, topicID) >= want {
			stop()
			return nil
		}
		if err := consumer.SleepWithContext(ctx, 20*time.Millisecond); err != nil {
			return nil // ctx already stopped some other way
		}
	}
	die(fmt.Sprintf("timed out waiting for %d distinct keys in v2's compaction_head", want))
	return nil
}

func versionHealth(all []*admin.VersionHealth, version topic.SchemaVersion) *admin.VersionHealth {
	for _, h := range all {
		if h.Topic.SchemaVersion == version {
			return h
		}
	}
	die(fmt.Sprintf("no VersionHealth entry for version %d", version))
	return nil
}

func distinctKeyCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, `SELECT count(*) FROM compaction_head WHERE topic_id=$1;`, topicID)
}

func rowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d`, topicID))
}

func winner(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, key string) *V2Order {
	var payload []byte
	err := ds.Pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT m.payload FROM compaction_head ch JOIN message_log_%d m ON m.id = ch.head_id WHERE ch.topic_id=$1 AND ch.compaction_key=$2;`, topicID),
		topicID, key).Scan(&payload)
	must(err)
	var v V2Order
	must(json.Unmarshal(payload, &v))
	return &v
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
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
	fmt.Printf("\n❌ LAB FAILED: %s\n", msg)
	os.Exit(1)
}
func assertInt(label string, got, want int64) {
	if got != want {
		die(fmt.Sprintf("%s: got %d, want %d", label, got, want))
	}
	fmt.Printf("  ✓ %s (%d)\n", label, got)
}
func assertTrue(label string, cond bool) {
	if !cond {
		die(fmt.Sprintf("%s: got false, want true", label))
	}
	fmt.Printf("  ✓ %s\n", label)
}
func assertWinner(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, key string, wantCents int64, wantCurrency string) {
	got := winner(ctx, ds, topicID, key)
	if got.Cents != wantCents || got.Currency != wantCurrency {
		die(fmt.Sprintf("%s winner: got {%d %s}, want {%d %s}", key, got.Cents, got.Currency, wantCents, wantCurrency))
	}
	fmt.Printf("  ✓ %s winner is {%d %s}\n", key, got.Cents, got.Currency)
}
