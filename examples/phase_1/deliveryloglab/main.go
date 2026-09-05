package main

// delivery_log lab: does the per-attempt audit trail actually behave like an
// audit trail -- one row per failed attempt, distinct rows (not overwrites)
// across retries -- and do the three delivery_log_mode settings and retention
// cleanup around it actually hold?
//
// Five scenarios, driven through the real consumer.Datastore methods (Commit,
// ClaimExceptions, RecordExceptionFailure, RecordExceptionSuccess,
// DropExpiredPartitions, SweepExpiredPartitions) rather than raw SQL:
//  1. under the default mode ('failures') a fresh failure logs exactly one
//     delivery_log row (attempt=0, the right error), a success in the same
//     Commit logs none.
//  2. retrying that same message twice logs two MORE distinct rows
//     (attempt=1, attempt=2) -- the PK is (consumer_group, message_id,
//     attempt), so a retry can never collide with or overwrite a prior one.
//  3. a topic registered with DeliveryLogModeOff silently skips every write
//     path (the table itself always exists, so re-enabling needs no DDL) --
//     a failure still writes its delivery row normally in delivery_<id>, just with no shadow
//     row.
//  4. a topic registered with DeliveryLogModeAll logs a 'success' row per
//     success, in the same txn as the success itself: Commit logs its
//     resolved successes, and an exception that later succeeds logs
//     'success' at its own attempt as its delivery row deletes.
//  5. retention (dropPartition's whole-partition removal, sweepBatch's
//     individually-expired-row reap) actually drains old delivery_log rows,
//     not just delivery_<id>'s.
//  6. a retry claim handed back at a busy key gate logs 'deferred' under the
//     number it returned, and the next claim's failure logs under that same
//     number beside it -- two events for one run, no key collision [0615].

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	iCommon "github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/consume"
	consumecontroller "github.com/agentstax/vulkan/pkg/consume/controller"
	exceptionconsumercontroller "github.com/agentstax/vulkan/pkg/consume/exceptionconsumer/controller"
	messageconsumercontroller "github.com/agentstax/vulkan/pkg/consume/messageconsumer/controller"
	iDatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/topic"
	janitordatastore "github.com/agentstax/vulkan/pkg/topic/janitor/controller/datastore"
	vulkan "github.com/agentstax/vulkan/pkg/vulkan"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	group     = "phase11.deliveryloglab"
	ttl       = 100 * time.Millisecond
	ttlMargin = 300 * time.Millisecond
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

	pool, err := vulkan.NewPostgresPool(ctx, "example_user", "example_password", "localhost", "example_db", nil)
	must(err)
	defer pool.Close()

	scenarioFreshFailureAndSuccess(ctx, pool)
	scenarioRetryDistinctAttempts(ctx, pool)
	scenarioDeliveryLogOff(ctx, pool)
	scenarioDeliveryLogAll(ctx, pool)
	scenarioRetentionDropPartition(ctx, pool)
	scenarioRetentionSweepBatch(ctx, pool)
	scenarioRedeferralSharesAttempt(ctx, pool)

	fmt.Println("\n✅ DELIVERY LOG LAB PASSED")
	fmt.Println("   a failure logs exactly one row, retries append distinct rows instead of")
	fmt.Println("   overwriting, mode 'off' skips every write entirely, mode 'all' logs a")
	fmt.Println("   'success' row per success in the success's own txn, and both retention")
	fmt.Println("   paths drain delivery_log the same as they already drain delivery_<id>.")
	return nil
}

// ---- scenario 1: fresh failure logs one row, success logs none ----

func scenarioFreshFailureAndSuccess(ctx context.Context, pool *pgxpool.Pool) {
	step("SCENARIO 1: a fresh failure logs one delivery_log row, a success logs none")

	tp, cd, wp, groupId := newTopic(ctx, pool, "scenario1", vulkan.TopicConfig{})
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()

	defer func() {
		must(client.Topic[common.Work](tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	seed(ctx, wp, 2)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, 2, 3, 5*time.Second, tp.DeliveryLogMode)
	must(err)
	if claim == nil || len(claim.Messages) != 2 {
		die("expected a fresh claim of 2 messages")
	}
	failingId, successId := claim.Messages[0].Id, claim.Messages[1].Id

	exceptions := []messageconsumercontroller.MessageOutcome{{MessageId: failingId, Kind: messageconsumercontroller.OutcomeException, Err: "simulated processing failure"}}
	must(cd.Commit(ctx, tp.Id, groupId, claim.Lease.Token, exceptions, 300*time.Millisecond, tp.DeliveryLogMode))

	assertDeliveryLogRow(ctx, ds, tp.Id, groupId, failingId, 0, "simulated processing failure", true)
	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, successId, 0)
	fmt.Println("PASS: failure logged exactly one row, success logged none")
}

// ---- scenario 2: two retries append two more distinct rows ----

func scenarioRetryDistinctAttempts(ctx context.Context, pool *pgxpool.Pool) {
	step("SCENARIO 2: retrying the same message twice appends attempt=1 then attempt=2, never overwrites")

	tp, cd, wp, groupId := newTopic(ctx, pool, "scenario2", vulkan.TopicConfig{})
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()
	exceptionConsumers, err := exceptionconsumercontroller.NewExceptionConsumerGroupController(ds, nil)
	must(err)

	defer func() {
		must(client.Topic[common.Work](tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	seed(ctx, wp, 1)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, 1, 3, 5*time.Second, tp.DeliveryLogMode)
	must(err)
	if claim == nil {
		die("expected a fresh claim")
	}
	failingId := claim.Messages[0].Id

	exceptions := []messageconsumercontroller.MessageOutcome{{MessageId: failingId, Kind: messageconsumercontroller.OutcomeException, Err: "attempt 0 failure"}}
	must(cd.Commit(ctx, tp.Id, groupId, claim.Lease.Token, exceptions, 300*time.Millisecond, tp.DeliveryLogMode))
	assertDeliveryLogRow(ctx, ds, tp.Id, groupId, failingId, 0, "attempt 0 failure", true)

	const maxAttempts = 5 // stays well below dead-letter for both retries below
	for _, attempt := range []int{1, 2} {
		time.Sleep(1500 * time.Millisecond) // outlives both the 300ms initial and CalculateDelay(0)=1s can_run_after
		claimed, err := exceptionConsumers.Claim(ctx, tp.Id, groupId, 1, 10, maxAttempts, 5*time.Second, tp.DeliveryLogMode)
		must(err)
		if len(claimed) != 1 || claimed[0].MessageId != failingId {
			die(fmt.Sprintf("expected to claim exactly message %d, got %+v", failingId, claimed))
		}
		errText := fmt.Sprintf("attempt %d failure", attempt)
		must(exceptionConsumers.RecordFailure(ctx, (&iCommon.RetryPolicy{MaxRetries: maxAttempts}).WithDefaults(), &claimed[0], fmt.Errorf("%s", errText), tp.DeliveryLogMode, nil))
		assertDeliveryLogRow(ctx, ds, tp.Id, groupId, failingId, attempt, errText, true)
	}

	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, failingId, 3) // attempt 0, 1, 2 -- three distinct rows
	fmt.Println("PASS: two retries appended two distinct rows, no overwrite of the original")
}

// ---- scenario 3: DeliveryLogModeOff skips every write ----

func scenarioDeliveryLogOff(ctx context.Context, pool *pgxpool.Pool) {
	step("SCENARIO 3: DeliveryLogModeOff skips every write (the table itself always exists)")

	tp, cd, wp, groupId := newTopic(ctx, pool, "scenario3", vulkan.TopicConfig{DeliveryLogMode: topic.DeliveryLogModeOff})
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()

	defer func() {
		must(client.Topic[common.Work](tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	// registration creates delivery_log_<id> regardless of the flag -- the
	// flag gates the writes, so re-enabling later needs no DDL
	assertTableExists(ctx, ds, fmt.Sprintf("%s.%s", ds.Schema, topic.DeliveryLogTable(tp.Id)), true)

	seed(ctx, wp, 1)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, 1, 3, 5*time.Second, tp.DeliveryLogMode)
	must(err)
	if claim == nil {
		die("expected a fresh claim")
	}
	failingId := claim.Messages[0].Id
	exceptions := []messageconsumercontroller.MessageOutcome{{MessageId: failingId, Kind: messageconsumercontroller.OutcomeException, Err: "should never be logged"}}
	must(cd.Commit(ctx, tp.Id, groupId, claim.Lease.Token, exceptions, 300*time.Millisecond, tp.DeliveryLogMode))

	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, failingId, 0) // the failure was never logged
	assertDeliveryRowCount(ctx, ds, tp.Id, 1)                     // the delivery row was still written
	fmt.Println("PASS: no delivery_log row written, failure delivery row still written normally in delivery_<id>, no error")
}

// ---- scenario 4: DeliveryLogModeAll logs successes in the success's own txn ----

func scenarioDeliveryLogAll(ctx context.Context, pool *pgxpool.Pool) {
	step("SCENARIO 4: DeliveryLogModeAll logs a 'success' row per success, same txn as the success")

	tp, cd, wp, groupId := newTopic(ctx, pool, "scenario4all", vulkan.TopicConfig{DeliveryLogMode: topic.DeliveryLogModeAll})
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()
	exceptionConsumers, err := exceptionconsumercontroller.NewExceptionConsumerGroupController(ds, nil)
	must(err)

	defer func() {
		must(client.Topic[common.Work](tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	seed(ctx, wp, 2)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, 2, 3, 5*time.Second, tp.DeliveryLogMode)
	must(err)
	if claim == nil || len(claim.Messages) != 2 {
		die("expected a fresh claim of 2 messages")
	}
	failingId, successId := claim.Messages[0].Id, claim.Messages[1].Id

	// one failure and one success in the same Commit -- the success rides the
	// outcome list as OutcomeSuccess, the shape the consumer runner uses
	// under this mode
	outcomes := []messageconsumercontroller.MessageOutcome{
		{MessageId: failingId, Kind: messageconsumercontroller.OutcomeException, Err: "scenario 4 failure"},
		{MessageId: successId, Kind: messageconsumercontroller.OutcomeSuccess},
	}
	must(cd.Commit(ctx, tp.Id, groupId, claim.Lease.Token, outcomes, 300*time.Millisecond, tp.DeliveryLogMode))

	assertDeliveryLogStatus(ctx, ds, tp.Id, groupId, successId, 0, "success")
	assertDeliveryLogStatus(ctx, ds, tp.Id, groupId, failingId, 0, "failure")

	// the unresolved exception now succeeds on its retry -- the delivery row's
	// deletion and its 'success' log row are one statement
	time.Sleep(1500 * time.Millisecond) // outlives the 300ms initial can_run_after
	claimed, err := exceptionConsumers.Claim(ctx, tp.Id, groupId, 1, 10, 5, 5*time.Second, tp.DeliveryLogMode)
	must(err)
	if len(claimed) != 1 || claimed[0].MessageId != failingId {
		die(fmt.Sprintf("expected to claim exactly message %d, got %+v", failingId, claimed))
	}
	must(exceptionConsumers.RecordSuccess(ctx, &claimed[0], tp.DeliveryLogMode, nil))

	assertDeliveryLogStatus(ctx, ds, tp.Id, groupId, failingId, claimed[0].Attempts, "success")
	assertDeliveryRowCount(ctx, ds, tp.Id, 0) // the success-deletion still happened
	fmt.Println("PASS: commit logged the success, the exception's later success logged at its own attempt")
}

// ---- scenario 5: retention drains old delivery_log rows ----

func scenarioRetentionDropPartition(ctx context.Context, pool *pgxpool.Pool) {
	step("SCENARIO 5a: dropPartition reaps a dormant message's delivery_log row")

	const partitionSize = int64(4)
	tp, cd, wp, groupId := newTopic(ctx, pool, "scenario4drop", vulkan.TopicConfig{PartitionSize: partitionSize})
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()
	janitorDatastore, err := janitordatastore.NewJanitorDatastore(ds, nil)
	must(err)

	defer func() {
		must(client.Topic[common.Work](tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	dormantId := failOne(ctx, cd, wp, tp, groupId, 4) // fills partition 0 (ids 1-4), fails id 1
	time.Sleep(ttl + ttlMargin)
	aliveId := failOne(ctx, cd, wp, tp, groupId, 4) // rolls into partition 1 (ids 5-8), fails id 5 -- well inside ttl

	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, dormantId, 1)
	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, aliveId, 1)

	must(janitorDatastore.DropExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, tp.DeliveryLogMode))

	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, dormantId, 0)
	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, aliveId, 1)
	fmt.Println("PASS: dropPartition reaped the dormant message's delivery_log row, left the alive one")
}

func scenarioRetentionSweepBatch(ctx context.Context, pool *pgxpool.Pool) {
	step("SCENARIO 5b: sweepBatch reaps a dormant message's delivery_log row individually")

	const partitionSize = int64(1000000) // never rolls -- exercises the sweep path instead of the drop
	tp, cd, wp, groupId := newTopic(ctx, pool, "scenario4sweep", vulkan.TopicConfig{PartitionSize: partitionSize})
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()
	janitorDatastore, err := janitordatastore.NewJanitorDatastore(ds, nil)
	must(err)

	defer func() {
		must(client.Topic[common.Work](tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	dormantId := failOne(ctx, cd, wp, tp, groupId, 1)
	time.Sleep(ttl + ttlMargin)
	aliveId := failOne(ctx, cd, wp, tp, groupId, 1) // well inside ttl

	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, dormantId, 1)
	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, aliveId, 1)

	must(janitorDatastore.SweepExpiredPartitions(ctx, tp.Id, partitionSize, ttl, true, 1000, tp.DeliveryLogMode))

	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, dormantId, 0)
	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, aliveId, 1)
	fmt.Println("PASS: sweepBatch reaped the dormant message's delivery_log row, left the alive one")
}

// ---- scenario 6: a gate re-deferral and the run after it share an attempt ----

func scenarioRedeferralSharesAttempt(ctx context.Context, pool *pgxpool.Pool) {
	step("SCENARIO 6: a claim handed back at the key gate and the next run log under the same attempt")

	tp, _, _, groupId := newTopic(ctx, pool, "scenario6", vulkan.TopicConfig{})
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)
	ds := client.Datastore()
	exceptionConsumers, err := exceptionconsumercontroller.NewExceptionConsumerGroupController(ds, nil)
	must(err)

	defer func() {
		must(client.Topic[common.Work](tp.Name).Destroy(ctx, &vulkan.DestroyOptions{Force: true}))
	}()

	// a keyed message with its first-delivery 'deferred' row, as the cursor path writes it
	var messageId int64
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`INSERT INTO %s.%s (message_key, schema_version, payload) VALUES ('k', 1, '{}') RETURNING id`, ds.Schema, topic.MessageLogTable(tp.Id))).Scan(&messageId))
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.%s (consumer_group_id, message_id, status, concurrency, attempts) VALUES ($1, $2, 'deferred', 'exclusive', 0)`, ds.Schema, topic.ExceptionQueueTable(tp.Id)), groupId, messageId)
	must(err)
	_, err = ds.Pool.Exec(ctx, fmt.Sprintf(`INSERT INTO %s.%s (consumer_group_id, message_id, attempt, status, error) VALUES ($1, $2, 0, 'deferred', '')`, ds.Schema, topic.DeliveryLogTable(tp.Id)), groupId, messageId)
	must(err)

	claimed, err := exceptionConsumers.Claim(ctx, tp.Id, groupId, 1, 10, 3, 5*time.Second, tp.DeliveryLogMode)
	must(err)
	if len(claimed) != 1 || claimed[0].Attempts != 1 {
		die(fmt.Sprintf("expected one claim at attempts 1, got %+v", claimed))
	}
	must(exceptionConsumers.RecordDeferred(ctx, &claimed[0], iCommon.ConcurrencyExclusive, tp.DeliveryLogMode))

	claimed, err = exceptionConsumers.Claim(ctx, tp.Id, groupId, 1, 10, 3, 5*time.Second, tp.DeliveryLogMode)
	must(err)
	if len(claimed) != 1 || claimed[0].Attempts != 1 {
		die(fmt.Sprintf("expected the handed-back number 1 to be claimed again, got %+v", claimed))
	}
	must(exceptionConsumers.RecordFailure(ctx, (&iCommon.RetryPolicy{MaxRetries: 3}).WithDefaults(), &claimed[0], fmt.Errorf("attempt 1 failure"), tp.DeliveryLogMode, nil))

	assertDeliveryLogStatusesAt(ctx, ds, tp.Id, groupId, messageId, 1, []string{"deferred", "failure"})
	assertDeliveryLogCount(ctx, ds, tp.Id, groupId, messageId, 3)
	fmt.Println("PASS: the gate deferral and the run after it both logged under attempt 1")
}

// ---- helpers ----

func newTopic(ctx context.Context, pool *pgxpool.Pool, suffix string, cfg vulkan.TopicConfig) (*topic.Topic, *messageconsumercontroller.MessageConsumerGroupController, *vulkan.ProducerInstance[common.Work], int64) {
	name := fmt.Sprintf("phase11.deliveryloglab.%s.%d", suffix, time.Now().UnixNano())
	client, err := vulkan.NewClient(ctx, pool, &vulkan.ClientConfig{AllowDestroy: true})
	must(err)

	ds := client.Datastore()
	tp, err := client.Topic[vulkan.RawPayload](name).Register(ctx, &cfg)
	must(err)

	cd, err := consumecontroller.NewConsumeController(ds, nil)
	must(err)
	groupId := mustGroupID(cd.RegisterGroup(ctx, tp.Id, group, consume.Beginning()))
	messageConsumers, err := messageconsumercontroller.NewMessageConsumerGroupController(ds, nil)
	must(err)
	wpInstance, err := client.Topic[common.Work](tp.Name).Producer().Register(ctx, nil)
	must(err)
	return tp, messageConsumers, wpInstance, groupId
}

func seed(ctx context.Context, wpInstance *vulkan.ProducerInstance[common.Work], n int) {
	for range n {
		_, err := wpInstance.ProduceFunc(ctx, func(ctx context.Context, tx vulkan.Tx) (*common.Work, error) {
			return common.NewWork(30, "admin@example.com")
		}, nil)
		must(err)
	}
}

// failOne claims a fresh range of n messages and fails the first one -- returns
// its id. Used by the retention scenarios, which only care about one failure
// per range, not the retry-distinctness scenario 2 already covers.
func failOne(ctx context.Context, cd *messageconsumercontroller.MessageConsumerGroupController, wpInstance *vulkan.ProducerInstance[common.Work], tp *topic.Topic, groupId int64, n int) int64 {
	seed(ctx, wpInstance, n)
	claim, err := cd.ClaimMessagesWithCursor(ctx, tp.Id, groupId, 1, n, 3, 5*time.Second, tp.DeliveryLogMode)
	must(err)
	if claim == nil {
		die("expected a fresh claim")
	}
	failingId := claim.Messages[0].Id
	exceptions := []messageconsumercontroller.MessageOutcome{{MessageId: failingId, Kind: messageconsumercontroller.OutcomeException, Err: "retention scenario failure"}}
	must(cd.Commit(ctx, tp.Id, groupId, claim.Lease.Token, exceptions, 300*time.Millisecond, tp.DeliveryLogMode))
	return failingId
}

func assertDeliveryLogRow(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64, attempt int, wantErr string, wantExists bool) {
	var gotErr string
	err := ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT error FROM %s.%s WHERE consumer_group_id = $1 AND message_id = $2 AND attempt = $3;`, ds.Schema, topic.DeliveryLogTable(topicId)), groupId, messageId, attempt).Scan(&gotErr)
	exists := err == nil
	if exists != wantExists {
		die(fmt.Sprintf("%s.%s[group=%d message=%d attempt=%d] exists=%v, want %v (err=%v)", ds.Schema, topic.DeliveryLogTable(topicId), groupId, messageId, attempt, exists, wantExists, err))
	}
	if wantExists && gotErr != wantErr {
		die(fmt.Sprintf("%s.%s[message=%d attempt=%d] error=%q, want %q", ds.Schema, topic.DeliveryLogTable(topicId), messageId, attempt, gotErr, wantErr))
	}
	fmt.Printf("  ✓ delivery_log_%d[message=%d attempt=%d] exists=%v%s\n", topicId, messageId, attempt, exists, errSuffix(wantExists, gotErr))
}

func errSuffix(wantExists bool, gotErr string) string {
	if !wantExists {
		return ""
	}
	return fmt.Sprintf(" error=%q", gotErr)
}

func assertDeliveryLogStatus(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64, attempt int, wantStatus string) {
	var gotStatus string
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT status FROM %s.%s WHERE consumer_group_id = $1 AND message_id = $2 AND attempt = $3;`, ds.Schema, topic.DeliveryLogTable(topicId)), groupId, messageId, attempt).Scan(&gotStatus))
	if gotStatus != wantStatus {
		die(fmt.Sprintf("%s.%s[message=%d attempt=%d] status=%q, want %q", ds.Schema, topic.DeliveryLogTable(topicId), messageId, attempt, gotStatus, wantStatus))
	}
	fmt.Printf("  ✓ delivery_log_%d[message=%d attempt=%d] status=%q\n", topicId, messageId, attempt, gotStatus)
}

// assertDeliveryLogStatusesAt checks every event logged under one attempt, in
// insertion order.
func assertDeliveryLogStatusesAt(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64, attempt int, want []string) {
	rows, err := ds.Pool.Query(ctx, fmt.Sprintf(`SELECT status FROM %s.%s WHERE consumer_group_id = $1 AND message_id = $2 AND attempt = $3 ORDER BY id;`, ds.Schema, topic.DeliveryLogTable(topicId)), groupId, messageId, attempt)
	must(err)
	defer rows.Close()
	var got []string
	for rows.Next() {
		var status string
		must(rows.Scan(&status))
		got = append(got, status)
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		die(fmt.Sprintf("%s.%s[message=%d attempt=%d] statuses=%v, want %v", ds.Schema, topic.DeliveryLogTable(topicId), messageId, attempt, got, want))
	}
	fmt.Printf("  ✓ delivery_log_%d[message=%d attempt=%d] statuses=%v\n", topicId, messageId, attempt, got)
}

func assertDeliveryLogCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, groupId int64, messageId int64, want int) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s WHERE consumer_group_id = $1 AND message_id = $2;`, ds.Schema, topic.DeliveryLogTable(topicId)), groupId, messageId).Scan(&count))
	if count != want {
		die(fmt.Sprintf("%s.%s[message=%d] has %d rows, want %d", ds.Schema, topic.DeliveryLogTable(topicId), messageId, count, want))
	}
	fmt.Printf("  ✓ delivery_log_%d[message=%d] has %d row(s)\n", topicId, messageId, count)
}

func assertDeliveryRowCount(ctx context.Context, ds *iDatastore.PostgresDatastore, topicId int64, want int) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.%s;`, ds.Schema, topic.ExceptionQueueTable(topicId))).Scan(&count))
	if count != want {
		die(fmt.Sprintf("%s.%s has %d rows, want %d", ds.Schema, topic.ExceptionQueueTable(topicId), count, want))
	}
	fmt.Printf("  ✓ exception_queue_%d has %d row(s)\n", topicId, count)
}

func assertTableExists(ctx context.Context, ds *iDatastore.PostgresDatastore, table string, want bool) {
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
	panic(labFailure{message: msg})
}

func mustGroupID(g *consume.Group, err error) int64 { must(err); return g.Id }
