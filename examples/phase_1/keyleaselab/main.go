// Command keyleaselab proves the key_lease primitives in isolation (no
// consumer wiring yet -- dispatch integration is proven by later labs).
//
// Registers its own topic, self-seeds keyed messages, fully self-contained.
//
// Confirms, in order:
//   - a stale (non-head) message resolves superseded WITHOUT creating or
//     touching a lease row -- the head check runs before the lease attempt,
//     both on a free key and while the key is held.
//   - the head message acquires when the key is free; a second attempt while
//     held is busy.
//   - a release inside a rolled-back txn leaves the lease held.
//   - a released key is immediately reacquirable.
//   - takeover only after expiry: before expires_at the key is busy, after
//     it the next claim wins with a FRESH token, and the expired holder's
//     release matches zero rows (it cannot delete the new holder's row).
//   - N concurrent acquires on one free key admit exactly one.
//   - old-then-new order while a key is held: a newer head version produced
//     mid-hold claims busy (the lease gates it, not the head), the held
//     message's own reclaim resolves superseded, and after release the new
//     head acquires.
//   - the janitor sweep removes expired rows and leaves live ones.
//   - destroying the topic sweeps its key_lease rows.
package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/agentstax/vulkan/pkg/admin"
	keyleasecontroller "github.com/agentstax/vulkan/pkg/consumer/base/controller"
	consumercontroller "github.com/agentstax/vulkan/pkg/consumer/controller"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/maintain"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	topiccontroller "github.com/agentstax/vulkan/pkg/topic/controller"
	"github.com/google/uuid"
)

const group = "keyleaselab.group"

type Rec struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
}

var (
	ds      *coredatastore.PostgresDatastore
	topicID int64
	groupID int64
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

	topicName := fmt.Sprintf("keyleaselab.%d", time.Now().UnixNano())
	tp, err := mAdmin.RegisterTopic(ctx, topicName, topic.SchemaVersion(1), &topiccontroller.TopicConfig{})
	must(err)
	topicID = tp.Id

	cd, err := consumercontroller.NewConsumerController(ds, nil)
	must(err)
	keyLeases, err := keyleasecontroller.NewKeyLeaseController(ds, nil)
	must(err)
	md, err := maintain.NewMaintenanceDatastore(ds, nil)
	must(err)
	wp, err := producer.NewProducer[Rec](ds, nil)
	must(err)
	wpInstance, err := wp.Register(ctx, tp.Name, topic.SchemaVersion(1))
	must(err)
	g, err := cd.RegisterGroup(ctx, tp.Id, group)
	must(err)
	groupID = g.Id

	step("seed: two versions of user:1 -- the newer is the compaction head")
	publish(ctx, wpInstance, "user:1", 1)
	publish(ctx, wpInstance, "user:1", 2)
	staleID := scalarInt64(ctx, fmt.Sprintf(`SELECT MIN(id) FROM message_log_%d WHERE compaction_key = 'user:1'`, topicID))
	headID := scalarInt64(ctx, `SELECT head_id FROM compaction_head WHERE topic_id = $1 AND compaction_key = 'user:1'`, topicID)
	if staleID == headID {
		die("seed broken: stale and head ids match")
	}
	fmt.Printf("  stale=%d head=%d\n", staleID, headID)

	step("stale message resolves superseded and never touches the lease row")
	c := claim(ctx, keyLeases, "user:1", staleID, 30*time.Second)
	if c.Verdict != keyleasecontroller.KeyLeaseSuperseded {
		die(fmt.Sprintf("want superseded, got %s", c.Verdict))
	}
	if n := leaseCount(ctx); n != 0 {
		die(fmt.Sprintf("superseded verdict created a lease row (count=%d) -- the head gate must run before the lease attempt", n))
	}
	fmt.Println("  ✓ superseded, zero lease rows")

	step("head message acquires when free; second attempt is busy")
	held := claim(ctx, keyLeases, "user:1", headID, 30*time.Second)
	if held.Verdict != keyleasecontroller.KeyLeaseAcquired || held.Token == uuid.Nil {
		die(fmt.Sprintf("want acquired with a token, got %s valid=%v", held.Verdict, held.Token != uuid.Nil))
	}
	if c := claim(ctx, keyLeases, "user:1", headID, 30*time.Second); c.Verdict != keyleasecontroller.KeyLeaseBusy {
		die(fmt.Sprintf("want busy while held, got %s", c.Verdict))
	}
	if n := leaseCount(ctx); n != 1 {
		die(fmt.Sprintf("want exactly 1 lease row, got %d", n))
	}
	fmt.Println("  ✓ acquired, then busy")

	step("stale message still resolves superseded while the key is held (gate beats busy)")
	if c := claim(ctx, keyLeases, "user:1", staleID, 30*time.Second); c.Verdict != keyleasecontroller.KeyLeaseSuperseded {
		die(fmt.Sprintf("want superseded (not busy) for a stale message on a held key, got %s", c.Verdict))
	}
	fmt.Println("  ✓ superseded takes precedence over busy")

	step("a release inside a rolled-back txn leaves the lease held")
	tx, err := ds.Pool.Begin(ctx)
	must(err)
	// mirrors releaseKeyLease's SQL (pkg/consumer/datastore.go) -- keep in sync
	tag, err := tx.Exec(ctx, `
		DELETE FROM key_lease
		WHERE consumer_group_id = $1
			AND compaction_key = $2
			AND lease_token = $3;
	`, groupID, "user:1", held.Token)
	must(err)
	if tag.RowsAffected() != 1 {
		die("the in-txn release should have matched the held row")
	}
	must(tx.Rollback(ctx))
	if c := claim(ctx, keyLeases, "user:1", headID, 30*time.Second); c.Verdict != keyleasecontroller.KeyLeaseBusy {
		die(fmt.Sprintf("want busy after rolled-back release, got %s", c.Verdict))
	}
	fmt.Println("  ✓ rollback kept the lease")

	step("release frees the key for immediate reacquire")
	released, err := keyLeases.ReleaseKeyLease(ctx, held)
	must(err)
	if !released {
		die("release of the live holder should match its row")
	}
	short := claim(ctx, keyLeases, "user:1", headID, 300*time.Millisecond)
	if short.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("want reacquire after release, got %s", short.Verdict))
	}
	fmt.Println("  ✓ released and reacquired")

	step("takeover only after expiry; the expired holder's release matches 0 rows")
	if c := claim(ctx, keyLeases, "user:1", headID, 30*time.Second); c.Verdict != keyleasecontroller.KeyLeaseBusy {
		die(fmt.Sprintf("want busy before expiry, got %s", c.Verdict))
	}
	time.Sleep(400 * time.Millisecond)
	taker := claim(ctx, keyLeases, "user:1", headID, 30*time.Second)
	if taker.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("want takeover after expiry, got %s", taker.Verdict))
	}
	if taker.Token == short.Token {
		die("takeover must mint a fresh token")
	}
	staleReleased, err := keyLeases.ReleaseKeyLease(ctx, short)
	must(err)
	if staleReleased {
		die("the expired holder's release matched a row -- it must not delete the new holder's row")
	}
	if n := leaseCount(ctx); n != 1 {
		die(fmt.Sprintf("the expired holder's release must not remove the new holder's row, count=%d", n))
	}
	takerReleased, err := keyLeases.ReleaseKeyLease(ctx, taker)
	must(err)
	if !takerReleased {
		die("the new holder's release should match its row")
	}
	fmt.Println("  ✓ busy before expiry, taken over after, expired token matched nothing")

	step("N concurrent acquires on one free key admit exactly one")
	const workers = 10
	var wg sync.WaitGroup
	results := make([]*keyleasecontroller.KeyLeaseClaim, workers)
	for i := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = claim(ctx, keyLeases, "user:1", headID, 30*time.Second)
		}()
	}
	wg.Wait()
	var winner *keyleasecontroller.KeyLeaseClaim
	acquired, busy := 0, 0
	for _, r := range results {
		switch r.Verdict {
		case keyleasecontroller.KeyLeaseAcquired:
			acquired++
			winner = r
		case keyleasecontroller.KeyLeaseBusy:
			busy++
		default:
			die(fmt.Sprintf("unexpected verdict %s in the race", r.Verdict))
		}
	}
	if acquired != 1 || busy != workers-1 {
		die(fmt.Sprintf("want exactly 1 winner, got acquired=%d busy=%d", acquired, busy))
	}
	released, err = keyLeases.ReleaseKeyLease(ctx, winner)
	must(err)
	if !released {
		die("race winner's release should match its row")
	}
	fmt.Printf("  ✓ %d racers, 1 winner\n", workers)

	step("old-then-new order: a newer head produced mid-hold waits for the release")
	publish(ctx, wpInstance, "user:3", 1)
	old3 := scalarInt64(ctx, `SELECT head_id FROM compaction_head WHERE topic_id = $1 AND compaction_key = 'user:3'`, topicID)
	holding := claim(ctx, keyLeases, "user:3", old3, 30*time.Second)
	if holding.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("want acquired on user:3, got %s", holding.Verdict))
	}
	publish(ctx, wpInstance, "user:3", 2)
	new3 := scalarInt64(ctx, `SELECT head_id FROM compaction_head WHERE topic_id = $1 AND compaction_key = 'user:3'`, topicID)
	if c := claim(ctx, keyLeases, "user:3", new3, 30*time.Second); c.Verdict != keyleasecontroller.KeyLeaseBusy {
		die(fmt.Sprintf("want busy for the new head while the old holds the key, got %s", c.Verdict))
	}
	if c := claim(ctx, keyLeases, "user:3", old3, 30*time.Second); c.Verdict != keyleasecontroller.KeyLeaseSuperseded {
		die(fmt.Sprintf("want superseded for the held message now that the head moved, got %s", c.Verdict))
	}
	released, err = keyLeases.ReleaseKeyLease(ctx, holding)
	must(err)
	if !released {
		die("the old holder's release should match its row")
	}
	after := claim(ctx, keyLeases, "user:3", new3, 30*time.Second)
	if after.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("want the new head to acquire after the release, got %s", after.Verdict))
	}
	released, err = keyLeases.ReleaseKeyLease(ctx, after)
	must(err)
	if !released {
		die("the new head's release should match its row")
	}
	fmt.Println("  ✓ new head waited out the old holder, then acquired")

	step("janitor sweep removes expired rows, leaves live ones")
	publish(ctx, wpInstance, "user:2", 1)
	head2 := scalarInt64(ctx, `SELECT head_id FROM compaction_head WHERE topic_id = $1 AND compaction_key = 'user:2'`, topicID)
	expired := claim(ctx, keyLeases, "user:1", headID, 50*time.Millisecond)
	if expired.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("sweep setup: want acquired, got %s", expired.Verdict))
	}
	live := claim(ctx, keyLeases, "user:2", head2, 30*time.Second)
	if live.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("sweep setup: want acquired, got %s", live.Verdict))
	}
	time.Sleep(100 * time.Millisecond)
	must(md.SweepExpiredKeyLeases(ctx, topicID, 1)) // batchSize 1 forces the batch loop
	if n := leaseCount(ctx); n != 1 {
		die(fmt.Sprintf("want only the live row to survive the sweep, count=%d", n))
	}
	survivor := scalarString(ctx, `SELECT compaction_key FROM key_lease WHERE consumer_group_id = $1`, groupID)
	if survivor != "user:2" {
		die(fmt.Sprintf("sweep removed the wrong row, survivor=%s", survivor))
	}
	fmt.Println("  ✓ expired swept, live kept")

	step("destroying the topic sweeps its key_lease rows")
	must(mAdmin.DestroyTopic(ctx, topicName, topic.SchemaVersion(1), admin.DestroyOptions{Force: true}))
	if n := leaseCount(ctx); n != 0 {
		die(fmt.Sprintf("destroy left %d key_lease rows behind", n))
	}
	fmt.Println("  ✓ destroy swept the rows")

	fmt.Println("\n✅ KEY LEASE LAB PASSED")
}

func claim(ctx context.Context, cd *keyleasecontroller.KeyLeaseController, key string, msgID int64, d time.Duration) *keyleasecontroller.KeyLeaseClaim {
	c, err := cd.ClaimKeyLease(ctx, topicID, groupID, key, msgID, d)
	must(err)
	return c
}

func publish(ctx context.Context, wpInstance *producer.ProducerInstance[Rec], key string, version int) {
	_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*Rec, error) {
		return &Rec{Key: key, Version: version}, nil
	}, producer.ProduceOptions{CompactionKey: key})
	must(err)
}

func leaseCount(ctx context.Context) int {
	var n int
	must(ds.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM key_lease WHERE consumer_group_id = $1`, groupID).Scan(&n))
	return n
}

func scalarInt64(ctx context.Context, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func scalarString(ctx context.Context, q string, args ...any) string {
	var v string
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
