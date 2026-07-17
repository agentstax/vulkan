package main

// delivery_log lab: does the per-attempt failure audit trail actually behave
// like an audit trail -- one row per failed attempt, none for successes,
// distinct rows (not overwrites) across retries -- and does the opt-out and
// retention cleanup around it actually hold?
//
// Four scenarios, driven through the real consumer.Datastore methods (Commit,
// ClaimExceptions, RecordExceptionFailure, DropExpiredPartitions,
// SweepExpiredPartitions) rather than raw SQL:
//  1. a fresh failure logs exactly one delivery_log row (attempt=0, the right
//     error), a success in the same Commit logs none.
//  2. retrying that same message twice logs two MORE distinct rows
//     (attempt=1, attempt=2) -- the PK is (consumer_group, message_id,
//     attempt), so a retry can never collide with or overwrite a prior one.
//  3. a topic registered with DisableDeliveryLog never even creates the
//     table, and every write path silently skips it -- a failure still
//     parks normally in delivery_<id>, just with no shadow row.
//  4. retention (dropPartition's whole-partition removal, sweepBatch's
//     individually-expired-row reap) actually drains old delivery_log rows,
//     not just delivery_<id>'s.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	"github.com/agentstax/vulkan/pkg/consumer"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
)

const (
	group     = "phase11.deliveryloglab"
	ttl       = 100 * time.Millisecond
	ttlMargin = 300 * time.Millisecond
)

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)

	scenarioFreshFailureAndSuccess(ctx, ds)
	scenarioRetryDistinctAttempts(ctx, ds)
	scenarioDisableDeliveryLog(ctx, ds)
	scenarioRetentionDropPartition(ctx, ds)
	scenarioRetentionSweepBatch(ctx, ds)

	fmt.Println("\n✅ DELIVERY LOG LAB PASSED")
	fmt.Println("   a failure logs exactly one row, a success logs none, retries append distinct")
	fmt.Println("   rows instead of overwriting, DisableDeliveryLog skips the table and every")
	fmt.Println("   write entirely, and both retention paths drain delivery_log the same as they")
	fmt.Println("   already drain delivery_<id>.")
}

// ---- scenario 1: fresh failure logs one row, success logs none ----

func scenarioFreshFailureAndSuccess(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("SCENARIO 1: a fresh failure logs one delivery_log row, a success logs none")

	tp, cd, wp := newTopic(ctx, ds, "scenario1", topic.Config{})
	defer func() { must(topic.Destroy(ctx, ds, tp.Name)) }()

	seed(ctx, wp, 2)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, 2, 3, 5*time.Second, tp.DisableDeliveryLog)
	must(err)
	if claim == nil || len(claim.Messages) != 2 {
		die("expected a fresh claim of 2 messages")
	}
	failingId, successId := claim.Messages[0].Id, claim.Messages[1].Id

	exceptions := []consumer.MessageException{{MessageId: failingId, Err: "simulated processing failure"}}
	must(cd.Commit(ctx, tp.Id, group, claim.Lease.Token, exceptions, nil, 300*time.Millisecond, tp.DisableDeliveryLog))

	assertDeliveryLogRow(ctx, ds, tp.Id, group, failingId, 0, "simulated processing failure", true)
	assertDeliveryLogCount(ctx, ds, tp.Id, group, successId, 0)
	fmt.Println("PASS: failure logged exactly one row, success logged none")
}

// ---- scenario 2: two retries append two more distinct rows ----

func scenarioRetryDistinctAttempts(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("SCENARIO 2: retrying the same message twice appends attempt=1 then attempt=2, never overwrites")

	tp, cd, wp := newTopic(ctx, ds, "scenario2", topic.Config{})
	defer func() { must(topic.Destroy(ctx, ds, tp.Name)) }()

	seed(ctx, wp, 1)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, 1, 3, 5*time.Second, tp.DisableDeliveryLog)
	must(err)
	if claim == nil {
		die("expected a fresh claim")
	}
	failingId := claim.Messages[0].Id

	exceptions := []consumer.MessageException{{MessageId: failingId, Err: "attempt 0 failure"}}
	must(cd.Commit(ctx, tp.Id, group, claim.Lease.Token, exceptions, nil, 300*time.Millisecond, tp.DisableDeliveryLog))
	assertDeliveryLogRow(ctx, ds, tp.Id, group, failingId, 0, "attempt 0 failure", true)

	const maxAttempts = 5 // stays well below dead-letter for both retries below
	for _, attempt := range []int{1, 2} {
		time.Sleep(1500 * time.Millisecond) // outlives both the 300ms initial and backoff(1)=1s can_run_after
		claimed, err := cd.ClaimExceptions(ctx, tp.Id, group, 10, maxAttempts, 5*time.Second, tp.DisableDeliveryLog)
		must(err)
		if len(claimed) != 1 || claimed[0].MessageId != failingId {
			die(fmt.Sprintf("expected to claim exactly message %d, got %+v", failingId, claimed))
		}
		errText := fmt.Sprintf("attempt %d failure", attempt)
		must(cd.RecordExceptionFailure(ctx, maxAttempts, &claimed[0], fmt.Errorf("%s", errText), tp.DisableDeliveryLog))
		assertDeliveryLogRow(ctx, ds, tp.Id, group, failingId, attempt, errText, true)
	}

	assertDeliveryLogCount(ctx, ds, tp.Id, group, failingId, 3) // attempt 0, 1, 2 -- three distinct rows
	fmt.Println("PASS: two retries appended two distinct rows, no overwrite of the original")
}

// ---- scenario 3: DisableDeliveryLog skips the table and every write ----

func scenarioDisableDeliveryLog(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("SCENARIO 3: DisableDeliveryLog skips table creation and every write")

	tp, cd, wp := newTopic(ctx, ds, "scenario3", topic.Config{DisableDeliveryLog: true})
	defer func() { must(topic.Destroy(ctx, ds, tp.Name)) }()

	assertTableExists(ctx, ds, fmt.Sprintf("delivery_log_%d", tp.Id), false)

	seed(ctx, wp, 1)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, 1, 3, 5*time.Second, tp.DisableDeliveryLog)
	must(err)
	if claim == nil {
		die("expected a fresh claim")
	}
	exceptions := []consumer.MessageException{{MessageId: claim.Messages[0].Id, Err: "should never be logged"}}
	must(cd.Commit(ctx, tp.Id, group, claim.Lease.Token, exceptions, nil, 300*time.Millisecond, tp.DisableDeliveryLog))

	assertTableExists(ctx, ds, fmt.Sprintf("delivery_log_%d", tp.Id), false) // still never created
	assertDeliveryRowCount(ctx, ds, tp.Id, 1)                                // the real park still happened
	fmt.Println("PASS: table never created, failure still parked normally in delivery_<id>, no error")
}

// ---- scenario 4: retention drains old delivery_log rows ----

func scenarioRetentionDropPartition(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("SCENARIO 4a: dropPartition reaps a dormant message's delivery_log row")

	const partitionSize = int64(4)
	tp, cd, wp := newTopic(ctx, ds, "scenario4drop", topic.Config{PartitionSize: partitionSize})
	defer func() { must(topic.Destroy(ctx, ds, tp.Name)) }()

	dormantId := failOne(ctx, cd, wp, tp, 4) // fills partition 0 (ids 1-4), fails id 1
	time.Sleep(ttl + ttlMargin)
	aliveId := failOne(ctx, cd, wp, tp, 4) // rolls into partition 1 (ids 5-8), fails id 5 -- well inside ttl

	assertDeliveryLogCount(ctx, ds, tp.Id, group, dormantId, 1)
	assertDeliveryLogCount(ctx, ds, tp.Id, group, aliveId, 1)

	must(cd.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, tp.DisableDeliveryLog))

	assertDeliveryLogCount(ctx, ds, tp.Id, group, dormantId, 0)
	assertDeliveryLogCount(ctx, ds, tp.Id, group, aliveId, 1)
	fmt.Println("PASS: dropPartition reaped the dormant message's delivery_log row, left the alive one")
}

func scenarioRetentionSweepBatch(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("SCENARIO 4b: sweepBatch reaps a dormant message's delivery_log row individually")

	const partitionSize = int64(1000000) // never rolls -- exercises the sweep path instead of the drop
	tp, cd, wp := newTopic(ctx, ds, "scenario4sweep", topic.Config{PartitionSize: partitionSize})
	defer func() { must(topic.Destroy(ctx, ds, tp.Name)) }()

	dormantId := failOne(ctx, cd, wp, tp, 1)
	time.Sleep(ttl + ttlMargin)
	aliveId := failOne(ctx, cd, wp, tp, 1) // well inside ttl

	assertDeliveryLogCount(ctx, ds, tp.Id, group, dormantId, 1)
	assertDeliveryLogCount(ctx, ds, tp.Id, group, aliveId, 1)

	must(cd.SweepExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, 1000, tp.DisableDeliveryLog))

	assertDeliveryLogCount(ctx, ds, tp.Id, group, dormantId, 0)
	assertDeliveryLogCount(ctx, ds, tp.Id, group, aliveId, 1)
	fmt.Println("PASS: sweepBatch reaped the dormant message's delivery_log row, left the alive one")
}

// ---- helpers ----

func newTopic(ctx context.Context, ds *coredatastore.PostgresDatastore, suffix string, cfg topic.Config) (*topic.Topic, consumer.Datastore[common.Work], *producer.MessageProducer[common.Work]) {
	cfg.Name = fmt.Sprintf("phase11.deliveryloglab.%s.%d", suffix, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, cfg)
	must(err)

	cd := consumer.NewConsumerDatastore[common.Work](ds, nil)
	must(cd.UpsertCursor(ctx, tp.Id, group))
	pd := producer.NewProducerDatastore[common.Work](ds, nil)
	wp := producer.NewMessageProducer(tp, pd)
	return tp, cd, wp
}

func seed(ctx context.Context, wp *producer.MessageProducer[common.Work], n int) {
	for range n {
		_, err := wp.Produce(ctx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, producer.ProduceOptions{})
		must(err)
	}
}

// failOne claims a fresh range of n messages and fails the first one -- returns
// its id. Used by the retention scenarios, which only care about one failure
// per range, not the retry-distinctness scenario 2 already covers.
func failOne(ctx context.Context, cd consumer.Datastore[common.Work], wp *producer.MessageProducer[common.Work], tp *topic.Topic, n int) int64 {
	seed(ctx, wp, n)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, group, n, 3, 5*time.Second, tp.DisableDeliveryLog)
	must(err)
	if claim == nil {
		die("expected a fresh claim")
	}
	failingId := claim.Messages[0].Id
	exceptions := []consumer.MessageException{{MessageId: failingId, Err: "retention scenario failure"}}
	must(cd.Commit(ctx, tp.Id, group, claim.Lease.Token, exceptions, nil, 300*time.Millisecond, tp.DisableDeliveryLog))
	return failingId
}

func assertDeliveryLogRow(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, consumerGroup string, messageID int64, attempt int, wantErr string, wantExists bool) {
	var gotErr string
	err := ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT error FROM delivery_log_%d WHERE consumer_group = $1 AND message_id = $2 AND attempt = $3;`, topicID), consumerGroup, messageID, attempt).Scan(&gotErr)
	exists := err == nil
	if exists != wantExists {
		die(fmt.Sprintf("delivery_log_%d[group=%s message=%d attempt=%d] exists=%v, want %v (err=%v)", topicID, consumerGroup, messageID, attempt, exists, wantExists, err))
	}
	if wantExists && gotErr != wantErr {
		die(fmt.Sprintf("delivery_log_%d[message=%d attempt=%d] error=%q, want %q", topicID, messageID, attempt, gotErr, wantErr))
	}
	fmt.Printf("  ✓ delivery_log_%d[message=%d attempt=%d] exists=%v%s\n", topicID, messageID, attempt, exists, errSuffix(wantExists, gotErr))
}

func errSuffix(wantExists bool, gotErr string) string {
	if !wantExists {
		return ""
	}
	return fmt.Sprintf(" error=%q", gotErr)
}

func assertDeliveryLogCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, consumerGroup string, messageID int64, want int) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM delivery_log_%d WHERE consumer_group = $1 AND message_id = $2;`, topicID), consumerGroup, messageID).Scan(&count))
	if count != want {
		die(fmt.Sprintf("delivery_log_%d[message=%d] has %d rows, want %d", topicID, messageID, count, want))
	}
	fmt.Printf("  ✓ delivery_log_%d[message=%d] has %d row(s)\n", topicID, messageID, count)
}

func assertDeliveryRowCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, want int) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM delivery_%d;`, topicID)).Scan(&count))
	if count != want {
		die(fmt.Sprintf("delivery_%d has %d rows, want %d", topicID, count, want))
	}
	fmt.Printf("  ✓ delivery_%d has %d row(s)\n", topicID, count)
}

func assertTableExists(ctx context.Context, ds *coredatastore.PostgresDatastore, table string, want bool) {
	var exists *string
	must(ds.Pool.QueryRow(ctx, `SELECT to_regclass($1)::text;`, table).Scan(&exists))
	got := exists != nil
	if got != want {
		die(fmt.Sprintf("%s exists=%v, want %v", table, got, want))
	}
	fmt.Printf("  ✓ %s exists=%v\n", table, got)
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
