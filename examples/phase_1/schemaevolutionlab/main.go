package main

// schema evolution bridge lab: the end-to-end proof of the payload-version
// design on one topic -- the reference implementation of the user-space
// BRIDGE pattern the library documents but doesn't ship a verb for.
//
// Scenario: one topic holds live keyed traffic written by a V1Order
// producer. The application evolves to V2Order (adds Currency). A bridge
// consumer group bound to V1Order reads each key's current head and
// re-produces it as V2Order at CompactionRank -1 (a backfill, never a live
// write), while the application's real producers write V2Order straight in
// at rank 0 for keys that have already cut over. Confirms, against the real
// claim/lease/cursor machinery, not just the SQL-level guarantee
// compactionranklab proves in isolation:
//   - a v2 row always beats the key's v1 head: the compaction winner compares
//     (schema_version, compaction_rank, head_id), so the bridge's rank -1 copy
//     supersedes the v1 row it was made from.
//   - zero-pause: a key with a live rank-0 v2 write never loses to the
//     bridge's rank -1 copy of the same key, in EITHER arrival order (user:1
//     is live-before-bridge, user:2 is bridge-before-live). A key whose
//     head already moved to v2 before the bridge reached it (user:1) is
//     never bridged at all: its v1 row stopped being the head, so the claim
//     skips it as superseded.
//   - crash + restart: stopping the bridge mid-drain and starting a fresh
//     instance on the same consumer group resumes from the persisted cursor
//     instead of re-walking the log from the start, and the source-id-derived
//     IdempotencyKey means however that boundary message gets settled (clean
//     commit vs. redelivered), exactly one bridged row lands per key -- never
//     a duplicate. This lab picks a deterministic stop point (distinct-key
//     count, not a timer) to stay non-flaky; it is not trying to win a race
//     against an in-flight commit -- idempotencykeysracelab already covers
//     dedup-under-true-concurrency.
//   - the retire verdict is a query: TopicHealth reports v1 safe once no
//     compaction head points at a v1 row and the bridge group has read past
//     every v1 row, and v2 not safe while the heads are at v2.
//
// Self-contained: registers the topic, destroys it on exit.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	consumermessage "github.com/agentstax/vulkan/pkg/consumergroup"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const group = "phase14a.schemaevolutionlab.bridge"

// bridgeNamespace seeds the bridge's UUIDv5 idempotency keys -- fixed so a
// given source message id always derives the same key, run to run.
var bridgeNamespace = uuid.MustParse("9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d")

var keys = []string{"user:1", "user:2", "user:3", "user:4", "user:5"}

// V1Order is what the producers wrote before the schema evolved.
type V1Order struct {
	Key   string `json:"key"`
	Cents int64  `json:"cents"`
}

func (V1Order) SchemaVersion() topic.SchemaVersion { return 1 }

// V2Order adds Currency -- the change the bridge exists to carry forward;
// rows the bridge itself writes default it to "USD".
type V2Order struct {
	Key      string `json:"key"`
	Cents    int64  `json:"cents"`
	Currency string `json:"currency"`
}

func (V2Order) SchemaVersion() topic.SchemaVersion { return 2 }

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

	ds, err := iDatastore.NewPostgresDatastore(ctx, "example_user", "localhost", "example_db", &iDatastore.PostgresConnectionConfig{Pass: "example_password"})
	must(err)
	defer ds.Close()

	mAdmin, err := admin.NewMessageAdmin(ds, &admin.MessageAdminConfig{AllowDestroy: true})
	must(err)
	must(mAdmin.RegisterSystem(ctx, nil))

	name := fmt.Sprintf("phase14a.schemaevolutionlab.%d", time.Now().UnixNano())
	registered, err := mAdmin.RegisterTopic(ctx, name, &topiccontroller.TopicConfig{})
	must(err)
	defer func() {
		must(mAdmin.DestroyTopic(ctx, name, admin.DestroyOptions{Force: true}))
	}()

	wp1, err := producer.NewProducer[V1Order](ds, nil)
	must(err)
	wp1Instance, err := wp1.Register(ctx, name)
	must(err)

	step("the topic holds live keyed V1Order traffic for 5 users")
	for i, key := range keys {
		cents := int64(i+1) * 100
		compaction, err := producer.NewCompactionOptions(0)
		must(err)
		_, err = wp1Instance.Produce(ctx, &V1Order{Key: key, Cents: cents}, producer.ProduceOptions{MessageKey: key, Compaction: compaction})
		must(err)
		fmt.Printf("  wrote %s cents=%d as V1Order\n", key, cents)
	}

	step("a V2Order producer registers on the same topic -- its rows carry schema_version 2")
	wp2, err := producer.NewProducer[V2Order](ds, nil)
	must(err)
	wp2Instance, err := wp2.Register(ctx, name)
	must(err)

	step("user:1 cuts over to v2 BEFORE the bridge ever sees it (live-then-backfill)")
	must(liveWrite(ctx, wp2Instance, "user:1", 999, "EUR"))

	// processed counts successful bridge writes; crashGate blocks the 3rd
	// message the bridge reaches (user:4 -- user:1 is already superseded)
	// until we've confirmed exactly 2 landed, then run1's
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

		meta, ok := consumermessage.MetaFromContext(ctx)
		if !ok {
			return fmt.Errorf("no MessageMeta in context for key %q", work.Key)
		}
		compaction, err := producer.NewCompactionOptions(-1)
		if err != nil {
			return err
		}
		_, err = wp2Instance.Produce(ctx, &V2Order{Key: work.Key, Cents: work.Cents, Currency: "USD"}, producer.ProduceOptions{
			MessageKey:     work.Key,
			Compaction:     compaction,
			IdempotencyKey: bridgeIdempotencyKey(meta.Id),
		})
		if err == nil {
			processed.Add(1)
		}
		return err
	}

	step("bridge run 1: skips superseded user:1, drains user:2 and user:3, then \"crashes\" mid-user:4")
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
	must(liveWrite(ctx, wp2Instance, "user:2", 888, "EUR"))
	close(crashGate) // release user:4, wherever it's stuck (fresh claim or a retried exception)

	step("bridge run 2: a fresh instance, same group, resumes from the persisted cursor")
	run2Ctx, cancelRun2 := context.WithCancel(ctx)
	bridge2 := newBridgeConsumer(ctx, ds, name)
	go func() {
		must(waitForCommitted(run2Ctx, ds, registered.Id, 10*time.Second, cancelRun2))
	}()
	must(bridge2.Consume(run2Ctx, bridgeFunc))

	step("verify the winners: live always beats the bridge, regardless of which arrived first")
	assertWinner(ctx, ds, registered.Id, "user:1", 999, "EUR") // live arrived first, still wins
	assertWinner(ctx, ds, registered.Id, "user:2", 888, "EUR") // live arrived second, still wins
	assertWinner(ctx, ds, registered.Id, "user:3", 300, "USD") // bridge only
	assertWinner(ctx, ds, registered.Id, "user:4", 400, "USD") // bridge only
	assertWinner(ctx, ds, registered.Id, "user:5", 500, "USD") // bridge only

	step("verify exactly-once: the crash/restart never left a duplicate row")
	assertInt("5 v1 rows, 4 bridged v2 rows (user:1 was superseded before the bridge reached it), 2 live v2 rows", rowCount(ctx, ds, registered.Id), 11)
	assertInt("every v1 row still physically present -- superseded, never rewritten", rowCountAtVersion(ctx, ds, registered.Id, 1), 5)

	step("the retire verdict is a query: v1 safe, v2 not")
	health, err := mAdmin.TopicHealth(ctx, name)
	must(err)
	v1Health := versionHealth(health, 1)
	assertInt("no compaction head still points at a v1 row", v1Health.CompactionHeads, 0)
	assertTrue("v1 is safe to retire", v1Health.Safe)
	fmt.Printf("  verdict v1: %s\n", v1Health.Reason)
	v2Health := versionHealth(health, 2)
	assertInt("every key's head is a v2 row", v2Health.CompactionHeads, int64(len(keys)))
	assertTrue("v2 is not safe to retire", !v2Health.Safe)
	fmt.Printf("  verdict v2: %s\n", v2Health.Reason)

	fmt.Println("\n✅ SCHEMA EVOLUTION BRIDGE LAB PASSED")
	fmt.Println("   a v2 row beats the v1 head -> live writes beat the bridge in either arrival order ->")
	fmt.Println("   a crashed bridge resumes from its cursor with no duplicates -> the retire verdict")
	fmt.Println("   reads the log instead of guessing.")
	return nil
}

// ---- helpers ----

func liveWrite(ctx context.Context, wp *producer.ProducerInstance[V2Order], key string, cents int64, currency string) error {
	compaction, err := producer.NewCompactionOptions(0)
	if err != nil {
		return err
	}
	_, err = wp.Produce(ctx, &V2Order{Key: key, Cents: cents, Currency: currency}, producer.ProduceOptions{MessageKey: key, Compaction: compaction})
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
func newBridgeConsumer(ctx context.Context, ds *iDatastore.PostgresDatastore, name string) *consumer.ConsumerInstance[V1Order] {
	c, err := consumer.NewConsumer[V1Order](ds, &consumer.ConsumerConfig{
		BatchLimit:              1,
		QueueSize:               4,
		MessageConcurrency:      1,
		ClaimPollRate:           50 * time.Millisecond,
		Message:                 &common.MessageOptions{Timeout: 2 * time.Second},
		QueueMargin:             500 * time.Millisecond,
		RecordMargin:            500 * time.Millisecond,
		ExceptionInitialBackoff: 200 * time.Millisecond,
		DisableGracefulShutdown: true,
	})
	must(err)
	cInstance, err := c.Register(ctx, group, name, nil)
	must(err)
	return cInstance
}

// waitForCommitted polls the bridge group's cursor until committed has
// passed the last v1 row -- a durable, DB-observed stop signal instead of a
// timer, taken after the commit so the retire verdict below reads a settled
// cursor.
func waitForCommitted(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, timeout time.Duration, stop context.CancelFunc) error {
	lastV1 := scalar(ctx, ds, fmt.Sprintf(`SELECT max(id) FROM message_log_%d WHERE schema_version = 1;`, topicId))
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if committed(ctx, ds, topicId) >= lastV1 {
			stop()
			return nil
		}
		select {
		case <-ctx.Done():
			return nil // ctx already stopped some other way
		case <-time.After(20 * time.Millisecond):
		}
	}
	die(fmt.Sprintf("timed out waiting for the bridge's committed cursor to reach %d", lastV1))
	return nil
}

func versionHealth(all []*admin.VersionHealth, version topic.SchemaVersion) *admin.VersionHealth {
	for _, h := range all {
		if h.Version == version {
			return h
		}
	}
	die(fmt.Sprintf("no VersionHealth entry for version %d", version))
	return nil
}

func committed(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`
		SELECT c.committed FROM consumer_group_cursor_%d c
		JOIN consumer_group_config g ON g.id = c.consumer_group_id
		WHERE g.name = $1;`, topicId), group)
}

func rowCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d`, topicId))
}

func rowCountAtVersion(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, version int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d WHERE schema_version = $1`, topicId), version)
}

func winner(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, key string) *V2Order {
	var payload []byte
	err := ds.Pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT m.payload FROM compaction_head_%d ch JOIN message_log_%d m ON m.id = ch.head_id WHERE ch.compaction_key=$1;`, topicId, topicId),
		key).Scan(&payload)
	must(err)
	var v V2Order
	must(json.Unmarshal(payload, &v))
	return &v
}

func scalar(ctx context.Context, ds *iDatastore.PostgresDatastore, q string, args ...any) int64 {
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
	panic(labFailure{message: msg})
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
func assertWinner(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, key string, wantCents int64, wantCurrency string) {
	got := winner(ctx, ds, topicId, key)
	if got.Cents != wantCents || got.Currency != wantCurrency {
		die(fmt.Sprintf("%s winner: got {%d %s}, want {%d %s}", key, got.Cents, got.Currency, wantCents, wantCurrency))
	}
	fmt.Printf("  ✓ %s winner is {%d %s}\n", key, got.Cents, got.Currency)
}
