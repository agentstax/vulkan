// Command keyleaselab proves the message_key_lease primitives in isolation (no
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
//   - destroying the topic drops its message_key_lease table.
package main

import (
	"context"
	"fmt"
	"github.com/agentstax/vulkan/pkg/topic"
	"os"
	"sync"
	"time"
	"uuid"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consumergroup"
	keyleasecontroller "github.com/agentstax/vulkan/pkg/consumergroup/base/controller"
	consumergroupcontroller "github.com/agentstax/vulkan/pkg/consumergroup/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	janitordatastore "github.com/agentstax/vulkan/pkg/topic/janitor/controller/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
)

const group = "keyleaselab.group"

type Rec struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
}

func (Rec) SchemaVersion() int { return 1 }

var (
	ds      *iDatastore.PostgresDatastore
	topicId int64
	groupId int64
)

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

	pool, err := iDatastore.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	ds, err = iDatastore.NewPostgresDatastore(ctx, pool, nil)
	must(err)

	client, err := vulkan.NewClient(ds, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	topicName := fmt.Sprintf("keyleaselab.%d", time.Now().UnixNano())
	tp, err := client.RegisterTopic(ctx, topicName, &vulkan.TopicConfig{})
	must(err)
	topicId = tp.Id

	cd, err := consumergroupcontroller.NewConsumerGroupController(ds, nil)
	must(err)
	keyLeases, err := keyleasecontroller.NewKeyLeaseController(ds, nil)
	must(err)
	janitorDatastore, err := janitordatastore.NewJanitorDatastore(ds, nil)
	must(err)
	wpInstance, err := client.RegisterProducer[Rec](ctx, tp.Name, nil)
	must(err)
	g, err := cd.RegisterGroup(ctx, tp.Id, group, consumergroup.Beginning())
	must(err)
	groupId = g.Id

	step("seed: two versions of user:1 -- the newer is the compaction head")
	publish(ctx, wpInstance, "user:1", 1)
	publish(ctx, wpInstance, "user:1", 2)
	staleID := scalarInt64(ctx, fmt.Sprintf(`SELECT MIN(id) FROM %s.%s WHERE message_key = 'user:1'`, ds.Schema, topic.MessageLogTable(topicId)))
	headID := scalarInt64(ctx, fmt.Sprintf(`SELECT head_id FROM %s.%s WHERE compaction_key = 'user:1'`, ds.Schema, topic.CompactionHeadTable(topicId)))
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
	if held.Verdict != keyleasecontroller.KeyLeaseAcquired || held.Token == uuid.Nil() {
		die(fmt.Sprintf("want acquired with a token, got %s valid=%v", held.Verdict, held.Token != uuid.Nil()))
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
	// mirrors consumerbase.release's SQL (pkg/consumergroup/base/controller/datastore/keylease.go) -- keep in sync
	tag, err := tx.Exec(ctx, fmt.Sprintf(`
		DELETE FROM %s.%s
		WHERE consumer_group_id = $1
			AND message_key = $2
			AND lease_token = $3;
	`, ds.Schema, topic.MessageKeyLeaseTable(topicId)), groupId, "user:1", held.Token)
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
	released, err := keyLeases.Release(ctx, held)
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
	staleReleased, err := keyLeases.Release(ctx, short)
	must(err)
	if staleReleased {
		die("the expired holder's release matched a row -- it must not delete the new holder's row")
	}
	if n := leaseCount(ctx); n != 1 {
		die(fmt.Sprintf("the expired holder's release must not remove the new holder's row, count=%d", n))
	}
	takerReleased, err := keyLeases.Release(ctx, taker)
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
	released, err = keyLeases.Release(ctx, winner)
	must(err)
	if !released {
		die("race winner's release should match its row")
	}
	fmt.Printf("  ✓ %d racers, 1 winner\n", workers)

	step("old-then-new order: a newer head produced mid-hold waits for the release")
	publish(ctx, wpInstance, "user:3", 1)
	old3 := scalarInt64(ctx, fmt.Sprintf(`SELECT head_id FROM %s.%s WHERE compaction_key = 'user:3'`, ds.Schema, topic.CompactionHeadTable(topicId)))
	holding := claim(ctx, keyLeases, "user:3", old3, 30*time.Second)
	if holding.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("want acquired on user:3, got %s", holding.Verdict))
	}
	publish(ctx, wpInstance, "user:3", 2)
	new3 := scalarInt64(ctx, fmt.Sprintf(`SELECT head_id FROM %s.%s WHERE compaction_key = 'user:3'`, ds.Schema, topic.CompactionHeadTable(topicId)))
	if c := claim(ctx, keyLeases, "user:3", new3, 30*time.Second); c.Verdict != keyleasecontroller.KeyLeaseBusy {
		die(fmt.Sprintf("want busy for the new head while the old holds the key, got %s", c.Verdict))
	}
	if c := claim(ctx, keyLeases, "user:3", old3, 30*time.Second); c.Verdict != keyleasecontroller.KeyLeaseSuperseded {
		die(fmt.Sprintf("want superseded for the held message now that the head moved, got %s", c.Verdict))
	}
	released, err = keyLeases.Release(ctx, holding)
	must(err)
	if !released {
		die("the old holder's release should match its row")
	}
	after := claim(ctx, keyLeases, "user:3", new3, 30*time.Second)
	if after.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("want the new head to acquire after the release, got %s", after.Verdict))
	}
	released, err = keyLeases.Release(ctx, after)
	must(err)
	if !released {
		die("the new head's release should match its row")
	}
	fmt.Println("  ✓ new head waited out the old holder, then acquired")

	step("janitor sweep removes expired rows, leaves live ones")
	publish(ctx, wpInstance, "user:2", 1)
	head2 := scalarInt64(ctx, fmt.Sprintf(`SELECT head_id FROM %s.%s WHERE compaction_key = 'user:2'`, ds.Schema, topic.CompactionHeadTable(topicId)))
	expired := claim(ctx, keyLeases, "user:1", headID, 50*time.Millisecond)
	if expired.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("sweep setup: want acquired, got %s", expired.Verdict))
	}
	live := claim(ctx, keyLeases, "user:2", head2, 30*time.Second)
	if live.Verdict != keyleasecontroller.KeyLeaseAcquired {
		die(fmt.Sprintf("sweep setup: want acquired, got %s", live.Verdict))
	}
	time.Sleep(100 * time.Millisecond)
	must(janitorDatastore.SweepExpiredKeyLeases(ctx, topicId, 1)) // batchSize 1 forces the batch loop
	if n := leaseCount(ctx); n != 1 {
		die(fmt.Sprintf("want only the live row to survive the sweep, count=%d", n))
	}
	survivor := scalarString(ctx, fmt.Sprintf(`SELECT message_key FROM %s.%s WHERE consumer_group_id = $1`, ds.Schema, topic.MessageKeyLeaseTable(topicId)), groupId)
	if survivor != "user:2" {
		die(fmt.Sprintf("sweep removed the wrong row, survivor=%s", survivor))
	}
	fmt.Println("  ✓ expired swept, live kept")

	step("destroying the topic drops its message_key_lease table")
	must(client.Topic(topicName).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	var keyLeaseTable *string
	must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1)::text;`, fmt.Sprintf("%s.%s", ds.Schema, topic.MessageKeyLeaseTable(topicId))).Scan(&keyLeaseTable))
	if keyLeaseTable != nil {
		die("destroy left the message_key_lease table behind")
	}
	fmt.Println("  ✓ destroy dropped the table")

	fmt.Println("\n✅ KEY LEASE LAB PASSED")
	return nil
}

func claim(ctx context.Context, cd *keyleasecontroller.KeyLeaseController, key string, msgID int64, d time.Duration) *keyleasecontroller.KeyLeaseClaim {
	c, err := cd.Claim(ctx, topicId, groupId, key, msgID, true, common.ConcurrencyExclusive, keyleasecontroller.RangeBounds{}, d)
	must(err)
	return c
}

func publish(ctx context.Context, wpInstance *vulkan.ProducerInstance[Rec], key string, version int) {
	compaction, err := vulkan.NewCompactionOptions(0)
	must(err)
	_, err = wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx, _ string) (*Rec, error) {
		return &Rec{Key: key, Version: version}, nil
	}, &vulkan.ProduceOptions{MessageKey: key, Compaction: compaction})
	must(err)
}

func leaseCount(ctx context.Context) int {
	var n int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1`, ds.Schema, topic.MessageKeyLeaseTable(topicId)), groupId).Scan(&n))
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
	panic(labFailure{message: msg})
}
