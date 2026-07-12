package main

// Log compaction lab: latest-per-key filtering at claim time.
//
// Registers its own topic (destroyed on exit), self-seeds, fully
// self-contained -- no dependency on external state.
//
// Confirms, in order:
//   - a claim spanning several versions of a compaction_key only returns the
//     latest; older versions still physically exist in message_log_<id>
//     (filtered at read time, never deleted).
//   - a version superseded AFTER its predecessor already delivered doesn't
//     retroactively unsend that predecessor -- committed only ever moves
//     forward, and the superseded row is still physically present.
//   - the crash/reclaim race directly: a worker claims a keyed row, crashes
//     before Commit, a newer version of that key lands, the lease expires
//     and gets reclaimed -- the reclaimed read now returns NOTHING (the
//     superseded row gets zero delivery, by design), and the newer version
//     still gets its own independent delivery later.
//   - a message whose own payload marks it deleted (a pure application
//     convention, not a framework concept) is still delivered normally on
//     both the CURSOR and LIFECYCLE paths.
//   - EXPLAIN ANALYZE over unkeyed-only rows shows the compaction subplan
//     never executes (the OR short-circuits on compaction_key IS NULL).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	cursorGroup    = "phase8c.compactionlab.cursor"
	lifecycleGroup = "phase8c.compactionlab.lifecycle"
)

// KeyedRecord is this lab's own payload shape. Deleted is the tombstone
// decision made real: the framework has zero opinion on it, a consumer just
// reads its own field like any other.
type KeyedRecord struct {
	Key     string `json:"key"`
	Version int    `json:"version"`
	Deleted bool   `json:"deleted,omitempty"`
}

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	topicName := fmt.Sprintf("phase8c.compactionlab.%d", time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, topic.Config{Name: topicName})
	must(err)
	defer func() { must(topic.Destroy(ctx, ds, topicName)) }()

	cd := consumer.NewConsumerDatastore[KeyedRecord](ds)
	pd := producer.NewProducerDatastore[KeyedRecord](ds)
	wp := producer.NewWorkProducer(tp, pd)
	must(cd.UpsertCursor(ctx, tp.Id, cursorGroup))

	const lease = 2 * time.Second
	const maxRangeReclaims = 3 // never exhausted in this lab -- exactly one reclaim happens

	// ===== latest-per-key survives, older rows stay physically present (ids 1-6) =====
	step("publish 3 versions of user:1, 1 unkeyed row, 2 versions of user:2")
	publish(ctx, wp, "user:1", 1, false) // id 1
	publish(ctx, wp, "user:1", 2, false) // id 2
	publish(ctx, wp, "user:1", 3, false) // id 3 <- latest for user:1
	publish(ctx, wp, "", 0, false)       // id 4, unkeyed -- never compacted
	publish(ctx, wp, "user:2", 1, false) // id 5
	publish(ctx, wp, "user:2", 2, false) // id 6 <- latest for user:2

	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroup, 10, maxRangeReclaims, lease)
	must(err)
	if claim == nil {
		die("expected a fresh claim, got nil (no work?)")
	}
	fmt.Printf("  claimed (%d,%d]  ids=%v\n", claim.Lease.Low, claim.Lease.High, ids(claim.Messages))
	assertIDs("only the latest version of each key, plus the unkeyed row, come back", ids(claim.Messages), []int64{3, 4, 6})
	assertInt("all 6 rows still physically exist -- compaction filters, never deletes", rowCount(ctx, ds, tp.Id), 6)

	must(cd.Commit(ctx, tp.Id, cursorGroup, claim.Lease.Token, nil, nil, 5*time.Second))
	committed := advance(ctx, cd, tp.Id)
	assertInt("committed advances over the whole range regardless of compaction", committed, 6)

	// ===== a delivered version isn't retroactively unsent once superseded (ids 7-8) =====
	step("user:3 v1 delivered, THEN v2 is published and delivered on its own later read")
	publish(ctx, wp, "user:3", 1, false) // id 7
	claim, err = cd.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroup, 1, maxRangeReclaims, lease)
	must(err)
	assertIDs("user:3 v1 delivered -- it's the only version so far", ids(claim.Messages), []int64{7})
	must(cd.Commit(ctx, tp.Id, cursorGroup, claim.Lease.Token, nil, nil, 5*time.Second))
	committed = advance(ctx, cd, tp.Id)
	assertInt("committed", committed, 7)

	publish(ctx, wp, "user:3", 2, false) // id 8, published AFTER v1 already delivered+committed
	claim, err = cd.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroup, 1, maxRangeReclaims, lease)
	must(err)
	assertIDs("user:3 v2 delivered on its own read -- v1's earlier delivery is untouched", ids(claim.Messages), []int64{8})
	must(cd.Commit(ctx, tp.Id, cursorGroup, claim.Lease.Token, nil, nil, 5*time.Second))
	committed = advance(ctx, cd, tp.Id)
	assertInt("committed only ever moves forward", committed, 8)
	assertTrue("v1 (id 7) is still physically present -- compaction never rewrites history", rowExists(ctx, ds, tp.Id, 7))

	// ===== the crash/reclaim race (ids 9-10) =====
	step("WORKER 1 claims user:4 v1, then crashes before Commit")
	publish(ctx, wp, "user:4", 1, false) // id 9
	claim1, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroup, 1, maxRangeReclaims, lease)
	must(err)
	if claim1 == nil {
		die("expected a fresh claim, got nil")
	}
	assertIDs("WORKER 1 claims user:4 v1", ids(claim1.Messages), []int64{9})
	fmt.Printf("  claimed (%d,%d] lease=%s -- WORKER 1 crashes here, never calls Commit\n",
		claim1.Lease.Low, claim1.Lease.High, shortTok(claim1.Lease.Token))

	step("a newer version of user:4 lands while v1 is still (unknowingly) in flight")
	publish(ctx, wp, "user:4", 2, false) // id 10

	step(fmt.Sprintf("sleep %s -- let the crashed lease expire", lease+500*time.Millisecond))
	time.Sleep(lease + 500*time.Millisecond)

	step("WORKER 2 polls: reclaims the exact expired range -- v1 is now superseded")
	claim2, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroup, 1, maxRangeReclaims, lease)
	must(err)
	if claim2 == nil {
		die("expected a reclaim, got nil")
	}
	assertInt("reclaim re-reads the exact same range low", claim2.Lease.Low, claim1.Lease.Low)
	assertInt("reclaim re-reads the exact same range high", claim2.Lease.High, claim1.Lease.High)
	if shortTok(claim2.Lease.Token) == shortTok(claim1.Lease.Token) {
		die("token was not rotated")
	}
	assertIDs("v1 is superseded -- the reclaimed read returns NOTHING for this range, by design", ids(claim2.Messages), []int64{})
	fmt.Println("  -> the accepted tradeoff: at-least-once is a per-KEY guarantee (the current latest")
	fmt.Println("     value eventually arrives), not a per-message one -- v1 owed nothing further")
	fmt.Println("     once v2 superseded it, exactly like Kafka's own compacted-topic contract")

	must(cd.Commit(ctx, tp.Id, cursorGroup, claim2.Lease.Token, nil, nil, 5*time.Second))
	committed = advance(ctx, cd, tp.Id)
	assertInt("committed moves past the (empty) reclaimed range", committed, 9)

	step("v2 still gets its own, independent delivery -- the obligation carried forward")
	claim3, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroup, 1, maxRangeReclaims, lease)
	must(err)
	assertIDs("user:4 v2 delivered", ids(claim3.Messages), []int64{10})
	must(cd.Commit(ctx, tp.Id, cursorGroup, claim3.Lease.Token, nil, nil, 5*time.Second))
	committed = advance(ctx, cd, tp.Id)
	assertInt("committed", committed, 10)

	// ===== tombstones are a pure app convention (ids 11-12) =====
	step("a message marked deleted in its OWN payload is delivered normally on both paths")
	publish(ctx, wp, "user:5", 1, true) // id 11, CURSOR path
	claim, err = cd.ClaimMessagesWithCursor(ctx, tp.Id, cursorGroup, 1, maxRangeReclaims, lease)
	must(err)
	assertIDs("CURSOR path delivers the deleted-marked message like any other", ids(claim.Messages), []int64{11})
	assertTrue("payload's own Deleted field survives -- the query never special-cases it", decode(claim.Messages[0].Payload).Deleted)
	must(cd.Commit(ctx, tp.Id, cursorGroup, claim.Lease.Token, nil, nil, 5*time.Second))
	committed = advance(ctx, cd, tp.Id)
	assertInt("committed", committed, 11)

	publish(ctx, wp, "user:6", 1, true) // id 12, LIFECYCLE path
	must(cd.FanOut(ctx, tp.Id, lifecycleGroup))
	delivered, err := cd.ClaimMessagesWithLifecycle(ctx, tp.Id, lifecycleGroup, 20)
	must(err)
	assertIDs("FanOut applies the identical unbounded predicate across the WHOLE topic, not a range",
		deliveryIDs(delivered), []int64{3, 4, 6, 8, 10, 11, 12})
	deletedDelivered := false
	for _, d := range delivered {
		if d.MessageId == 12 {
			deletedDelivered = decode(d.Payload).Deleted
		}
	}
	assertTrue("LIFECYCLE path also delivers the deleted-marked message", deletedDelivered)

	// ===== EXPLAIN: unkeyed-only traffic never pays the compaction subplan =====
	step("EXPLAIN ANALYZE: an unkeyed-only read never executes the compaction subplan")
	for range 5 {
		publish(ctx, wp, "", 0, false) // ids 13-17
	}
	explainNoCompactionSubplan(ctx, ds, tp.Id, 12, 17)

	fmt.Println("\n✅ COMPACTION LAB PASSED")
	fmt.Println("   latest-per-key survives, older rows persist untouched -> a delivered version stays")
	fmt.Println("   delivered even once superseded -> a crashed-then-superseded row gets zero delivery")
	fmt.Println("   while its successor still gets its own -> tombstones are pure app convention on both")
	fmt.Println("   paths -> unkeyed reads never pay the compaction subplan's cost.")
}

// ---- helpers ----

func publish(ctx context.Context, wp *producer.WorkProducer[KeyedRecord], key string, version int, deleted bool) {
	_, err := wp.Produce(ctx, func(ctx context.Context, tx pgx.Tx, _ uuid.UUID) (*KeyedRecord, error) {
		return &KeyedRecord{Key: key, Version: version, Deleted: deleted}, nil
	}, producer.ProduceOptions{CompactionKey: key})
	must(err)
}

func advance(ctx context.Context, cd consumer.Datastore[KeyedRecord], topicID int64) int64 {
	c, err := cd.AdvanceWaterline(ctx, topicID, cursorGroup)
	must(err)
	return c
}

func decode(payload json.RawMessage) KeyedRecord {
	var kr KeyedRecord
	must(json.Unmarshal(payload, &kr))
	return kr
}

// explainNoCompactionSubplan EXPLAIN ANALYZEs the exact shape readMessages runs
// over an id range that only contains unkeyed rows, then checks the plan for
// the latest_keys lookup being marked never executed -- proof the OR's left
// disjunct (compaction_key IS NULL) short-circuited it for every row.
func explainNoCompactionSubplan(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, low, high int64) {
	logTable := fmt.Sprintf("message_log_%d", topicID)
	sql := fmt.Sprintf(`
		EXPLAIN (ANALYZE, COSTS OFF, TIMING OFF) SELECT m.id, m.payload, m.created_at FROM %s m
		WHERE m.id > $1
			AND m.id <= $2
			AND (
				NOT EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group = $3)
				OR EXISTS (SELECT 1 FROM bindings b WHERE b.consumer_group = $3 AND m.routing_key ~ b.pattern)
			)
			AND (
				m.compaction_key IS NULL
				OR m.id = (SELECT latest_id FROM latest_keys
					WHERE topic_id = %d AND compaction_key = m.compaction_key)
			)
		ORDER BY m.id;
	`, logTable, topicID)

	rows, err := ds.Pool.Query(ctx, sql, low, high, cursorGroup)
	must(err)
	defer rows.Close()

	var plan strings.Builder
	for rows.Next() {
		var line string
		must(rows.Scan(&line))
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	must(rows.Err())
	fmt.Print(plan.String())

	matched, err := regexp.MatchString(`(?i)latest_keys.*never executed`, plan.String())
	must(err)
	assertTrue("the latest_keys lookup never executed against unkeyed-only rows", matched)
}

func rowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64) int64 {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d`, topicID))
}

func rowExists(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID, id int64) bool {
	return scalar(ctx, ds, fmt.Sprintf(`SELECT count(*) FROM message_log_%d WHERE id=$1`, topicID), id) == 1
}

func scalar(ctx context.Context, ds *coredatastore.PostgresDatastore, q string, args ...any) int64 {
	var v int64
	must(ds.Pool.QueryRow(ctx, q, args...).Scan(&v))
	return v
}

func ids(msgs []consumer.MessageRow) []int64 {
	out := make([]int64, len(msgs))
	for i, m := range msgs {
		out[i] = m.Id
	}
	return out
}

func deliveryIDs(rows []consumer.DeliveryRow) []int64 {
	out := make([]int64, len(rows))
	for i, r := range rows {
		out[i] = r.MessageId
	}
	return out
}

func shortTok[T fmt.Stringer](t T) string {
	s := t.String()
	if len(s) >= 8 {
		return s[:8]
	}
	return s
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
func assertIDs(label string, got, want []int64) {
	if len(got) != len(want) {
		die(fmt.Sprintf("%s: got %v, want %v", label, got, want))
	}
	for i := range got {
		if got[i] != want[i] {
			die(fmt.Sprintf("%s: got %v, want %v", label, got, want))
		}
	}
	fmt.Printf("  ✓ %s %v\n", label, got)
}
func assertTrue(label string, cond bool) {
	if !cond {
		die(fmt.Sprintf("%s: got false, want true", label))
	}
	fmt.Printf("  ✓ %s\n", label)
}
