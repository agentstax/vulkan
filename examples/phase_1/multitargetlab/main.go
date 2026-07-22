package main

// multi-target transactional enqueue lab: does producer.InTransaction +
// MessageProducer.ProduceInTx actually deliver the atomicity/isolation
// guarantees the design promises?
//
// Four scenarios:
//   - atomicPublishScenario: two targets published inside one InTransaction
//     closure both land together on success.
//   - rollbackOnFailureScenario: the second target's producerFunc returning
//     an error rolls back the WHOLE transaction, not just that target --
//     the first target's insert never lands either.
//   - partitionSelfHealIsolationScenario: forcing a missing-partition retry
//     on the second target must not touch the first target's already-made
//     insert, and must not rerun a caller side effect that already fired
//     between the two ProduceInTx calls.
//   - ambiguousCommitScenario: a Commit-time failure (a deferred FK
//     violation, so it surfaces at Commit, not at any INSERT) comes back
//     from InTransaction completely unclassified -- no retry.PermanentError
//     wrapping, no special-casing. InTransaction never retries; whether a
//     rerun is safe is the caller's call, so the raw error must reach them.
//   - callerKeyRetryScenario: the sanctioned way to make that rerun safe --
//     caller-supplied IdempotencyKeys per target. Rerunning the whole
//     closure under the same keys (a retry after a lost Commit ack) dedups
//     every target instead of double-publishing.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentstax/vulkan/examples/phase_1/common"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

var fn = func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
	return common.NewWork(30, "admin@example.com")
}

func main() {
	ctx := context.Background()

	ds, err := coredatastore.NewPostgresDatastore(ctx, &coredatastore.PostgresConnectionConfig{
		User: "example_user", Pass: "example_password",
		Host: "localhost", Port: 5432, Database: "example_db",
	})
	must(err)
	defer ds.Close()

	atomicPublishScenario(ctx, ds)
	rollbackOnFailureScenario(ctx, ds)
	partitionSelfHealIsolationScenario(ctx, ds)
	ambiguousCommitScenario(ctx, ds)
	callerKeyRetryScenario(ctx, ds)

	fmt.Println("\n✅ MULTI-TARGET LAB PASSED")
	fmt.Println("   two targets in one InTransaction closure commit together, a failure on")
	fmt.Println("   either rolls back both, a missing-partition self-heal on one target")
	fmt.Println("   never touches the other's work or reruns a side effect between them, a")
	fmt.Println("   Commit-time failure surfaces completely unclassified, and rerunning the")
	fmt.Println("   closure under caller-supplied keys dedups instead of double-publishing.")
}

func atomicPublishScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("atomic publish: two targets in one InTransaction closure both land together")

	topicA, wpA, cleanupA := newTarget(ctx, ds, "a", 1000)
	defer cleanupA()
	topicB, wpB, cleanupB := newTarget(ctx, ds, "b", 1000)
	defer cleanupB()

	err := producer.InTransaction(ctx, ds, func(ctx context.Context, tx producer.Tx) error {
		if _, err := wpA.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{}); err != nil {
			return err
		}
		_, err := wpB.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{})
		return err
	})
	must(err)

	assertMessageLogCount(ctx, ds, topicA.Id, 1)
	assertMessageLogCount(ctx, ds, topicB.Id, 1)
	fmt.Println("  ✓ both targets committed together")
}

func rollbackOnFailureScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("rollback on failure: second target's producerFunc erroring rolls back BOTH, not just itself")

	topicA, wpA, cleanupA := newTarget(ctx, ds, "a", 1000)
	defer cleanupA()
	topicB, wpB, cleanupB := newTarget(ctx, ds, "b", 1000)
	defer cleanupB()

	wantErr := errors.New("second target refuses to publish")
	err := producer.InTransaction(ctx, ds, func(ctx context.Context, tx producer.Tx) error {
		if _, err := wpA.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{}); err != nil {
			return err
		}
		_, err := wpB.ProduceInTx(ctx, tx, func(ctx context.Context, tx producer.Tx, _ uuid.UUID) (*common.Work, error) {
			return nil, wantErr
		}, producer.ProduceOptions{})
		return err
	})
	if !errors.Is(err, wantErr) {
		die(fmt.Sprintf("InTransaction returned %v, want %v surfaced as-is", err, wantErr))
	}

	assertMessageLogCount(ctx, ds, topicA.Id, 0)
	assertMessageLogCount(ctx, ds, topicB.Id, 0)
	fmt.Println("  ✓ target A's insert never lands either -- one shared tx, not two independent publishes")
}

func partitionSelfHealIsolationScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("partition self-heal isolation: B's internal retry must not touch A's work or rerun a side effect between them")

	topicA, wpA, cleanupA := newTarget(ctx, ds, "a", 1000)
	defer cleanupA()
	// partitionSize=2 -- one seeded row fills partition_0 [0,2) exactly
	// (BIGSERIAL starts at 1), so the NEXT id has nowhere to land yet.
	topicB, wpB, cleanupB := newTarget(ctx, ds, "b", 2)
	defer cleanupB()

	_, err := wpB.ProduceFunc(ctx, fn, producer.ProduceOptions{})
	must(err)
	assertMessageLogCount(ctx, ds, topicB.Id, 1)

	betweenCalls := 0
	err = producer.InTransaction(ctx, ds, func(ctx context.Context, tx producer.Tx) error {
		if _, err := wpA.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{}); err != nil {
			return err
		}
		betweenCalls++                                                    // stands in for a caller side effect like sendEmailConfirmation
		_, err := wpB.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{}) // misses its partition, self-heals
		return err
	})
	must(err)

	if betweenCalls != 1 {
		die(fmt.Sprintf("side effect between targets fired %d times, want exactly 1 -- B's self-heal retry must not rerun anything before it", betweenCalls))
	}
	assertMessageLogCount(ctx, ds, topicA.Id, 1)
	assertMessageLogCount(ctx, ds, topicB.Id, 2) // 1 seeded + 1 self-healed into a fresh partition
	fmt.Println("  ✓ A's insert survives untouched, the side effect between calls fired exactly once, B self-healed and landed")
}

func ambiguousCommitScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("ambiguous commit: a Commit-time failure surfaces unclassified -- retrying is the caller's decision")

	setupDeferredFKFixture(ctx, ds)
	defer teardownDeferredFKFixture(ctx, ds)

	topicA, wpA, cleanupA := newTarget(ctx, ds, "a", 1000)
	defer cleanupA()
	topicB, wpB, cleanupB := newTarget(ctx, ds, "b", 1000)
	defer cleanupB()

	err := producer.InTransaction(ctx, ds, func(ctx context.Context, tx producer.Tx) error {
		if _, err := wpA.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{}); err != nil {
			return err
		}
		if _, err := wpB.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{}); err != nil {
			return err
		}
		// passes now (deferred) -- fails when Commit checks the constraint
		_, err := tx.Exec(ctx, "INSERT INTO multitargetlab_deferred_child (parent_id) VALUES (-1);")
		return err
	})

	pgErr, ok := errors.AsType[*pgconn.PgError](err)
	if !ok || pgErr.Code != "23503" {
		die(fmt.Sprintf("expected the raw foreign_key_violation (23503) from tx.Commit, got %v", err))
	}
	if _, ok := errors.AsType[*retry.PermanentError](err); ok {
		die("InTransaction wrapped the commit error in retry.PermanentError -- it must never classify, only surface as-is")
	}

	assertMessageLogCount(ctx, ds, topicA.Id, 0)
	assertMessageLogCount(ctx, ds, topicB.Id, 0)

	fmt.Println("  ✓ Commit-time failure surfaces as the raw driver error, unclassified")
}

// callerKeyRetryScenario: reruns the whole closure under the SAME caller
// keys -- what a caller does after losing a Commit ack. Auto-minted keys
// resolve fresh per call, so THIS dedup guarantee belongs to caller keys
// alone: without them a closure rerun double-publishes every target.
func callerKeyRetryScenario(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	step("caller-key retry: rerunning the closure under the same keys dedups every target")

	topicA, wpA, cleanupA := newTarget(ctx, ds, "a", 1000)
	defer cleanupA()
	topicB, wpB, cleanupB := newTarget(ctx, ds, "b", 1000)
	defer cleanupB()

	keyA := uuid.Must(uuid.NewV7())
	keyB := uuid.Must(uuid.NewV7())

	closure := func(ctx context.Context, tx producer.Tx) error {
		if _, err := wpA.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{IdempotencyKey: keyA}); err != nil {
			return err
		}
		_, err := wpB.ProduceInTx(ctx, tx, fn, producer.ProduceOptions{IdempotencyKey: keyB})
		return err
	}

	must(producer.InTransaction(ctx, ds, closure)) // the publish whose ack was "lost"
	must(producer.InTransaction(ctx, ds, closure)) // the caller's retry

	assertMessageLogCount(ctx, ds, topicA.Id, 1)
	assertMessageLogCount(ctx, ds, topicB.Id, 1)
	fmt.Println("  ✓ both targets landed exactly once across two full closure runs")
}

// ---- fixtures ----

func newTarget(ctx context.Context, ds *coredatastore.PostgresDatastore, label string, partitionSize int64) (*topic.Topic, *producer.MessageProducer[common.Work], func()) {
	name := fmt.Sprintf("multitargetlab.%s.%d", label, time.Now().UnixNano())
	tp, err := topic.Register(ctx, ds, &topic.Config{Name: name, PartitionSize: partitionSize})
	must(err)

	wp, err := producer.NewMessageProducer[common.Work](tp, ds, &producer.MessageProducerConfig{DisableGracefulShutdown: true})
	must(err)
	must(wp.Register(ctx))
	return tp, wp, func() { must(topic.Destroy(ctx, ds, name)) }
}

// setupDeferredFKFixture builds a scratch FK relationship whose violation is
// only checked at COMMIT, not at INSERT -- the only way to force a genuine
// Commit-time failure on demand.
func setupDeferredFKFixture(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	must(exec(ctx, ds, `CREATE TABLE IF NOT EXISTS multitargetlab_deferred_parent (id BIGINT PRIMARY KEY);`))
	must(exec(ctx, ds, `
		CREATE TABLE IF NOT EXISTS multitargetlab_deferred_child (
			id BIGSERIAL PRIMARY KEY,
			parent_id BIGINT NOT NULL,
			CONSTRAINT multitargetlab_fk FOREIGN KEY (parent_id)
				REFERENCES multitargetlab_deferred_parent(id) DEFERRABLE INITIALLY DEFERRED
		);
	`))
}

func teardownDeferredFKFixture(ctx context.Context, ds *coredatastore.PostgresDatastore) {
	must(exec(ctx, ds, `DROP TABLE IF EXISTS multitargetlab_deferred_child;`))
	must(exec(ctx, ds, `DROP TABLE IF EXISTS multitargetlab_deferred_parent;`))
}

func exec(ctx context.Context, ds *coredatastore.PostgresDatastore, sql string) error {
	_, err := ds.Pool.Exec(ctx, sql)
	return err
}

// ---- helpers ----

func assertMessageLogCount(ctx context.Context, ds *coredatastore.PostgresDatastore, topicID int64, want int) {
	var count int
	must(ds.Pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM message_log_%d;`, topicID)).Scan(&count))
	if count != want {
		die(fmt.Sprintf("message_log_%d has %d rows, want %d", topicID, count, want))
	}
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
