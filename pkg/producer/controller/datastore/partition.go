package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ddlLockTimeout caps how long the self-heal CREATE waits for its table lock.
// WAITING is the hazard, not failing: postgres queues every later produce and
// claim behind the lock, so a stuck lock holder would stall the whole topic --
// better to fail this produce fast and let the caller's retry policy own it.
const ddlLockTimeout = 2 * time.Second

// createAheadAttemptAllowance is one attempt's share of the create-ahead
// timeout: two lock_timeout-bounded waits (advisory lock, CREATE) plus
// round-trip slack.
const createAheadAttemptAllowance = 3 * ddlLockTimeout

// insertUntilCovered runs insert until a partition covers it. An insert
// learns its ids from the sequence, so a rerun can land past the partition
// the previous heal created; each heal covers the sequence's next id, so
// the loop only continues while other producers advanced the sequence a
// whole partition in between. One heal for the boundary itself, then one
// per configured retry.
func (d *ProducerDatastore) insertUntilCovered(ctx context.Context, topicId int64, partitionSize int64, insert func() error) error {
	for heals := 0; ; heals++ {
		err := insert()
		if !isMissingPartition(err) {
			return err
		}
		if heals > d.DatastoreRetry.MaxRetries {
			return errPartitionCreationBehind.Wrap(err).With("topic_id", topicId)
		}

		if err := d.createNextIdPartition(ctx, topicId, partitionSize); err != nil {
			return err
		}
	}
}

// createNextIdPartition creates the partition the next id will land in.
// can't use the passed id as that id is already likely burned from an
// attempt in the sequence table.
func (d *ProducerDatastore) createNextIdPartition(ctx context.Context, topicId int64, partitionSize int64) error {
	lastValueSql := fmt.Sprintf(`
		-- vulkan: producer.createNextIdPartition
		SELECT last_value FROM %s;
	`, topic.MessageLogIdSequence(topicId))

	var lastValue int64
	if err := d.Datastore.Pool.QueryRow(ctx, lastValueSql).Scan(&lastValue); err != nil {
		return err
	}

	next := lastValue + 1
	d.Logger.WarnContext(ctx, eventPartitionCreatedOnInsert.Message, "code", eventPartitionCreatedOnInsert.Code, "topic_id", topicId, "message_id", next)

	return d.ensureCoveringPartition(ctx, topicId, partitionSize, next)
}

// ensureCoveringPartition creates the partition that covers id.
func (d *ProducerDatastore) ensureCoveringPartition(ctx context.Context, topicId int64, partitionSize int64, id int64) error {
	next := id / partitionSize

	createPartitionSql := fmt.Sprintf(`
		-- vulkan: producer.ensureCoveringPartition
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (%d) TO (%d);
	`, topic.MessageLogPartitionTable(topicId, next), topic.MessageLogTable(topicId), next*partitionSize, (next+1)*partitionSize)

	lockKey, err := common.NewAdvisoryLockKey("partition", d.Datastore.Schema, topicId, next)
	if err != nil {
		return err
	}

	// one round trip -- a batch outside an explicit txn runs as one implicit
	// transaction, which scopes the SET LOCAL to exactly these statements
	// instead of leaking it to whatever might use this pooled connection next,
	// and releases the advisory lock at commit
	batch := &pgx.Batch{}
	batch.Queue(fmt.Sprintf(`
		-- vulkan: producer.ensureCoveringPartition
		SET LOCAL lock_timeout = '%dms';
	`, ddlLockTimeout.Milliseconds()))

	// one winner runs the CREATE; every concurrent caller sleeps here (bounded
	// by the lock_timeout above) until that commit.
	batch.Queue(`
		-- vulkan: producer.ensureCoveringPartition
		SELECT pg_advisory_xact_lock($1);
	`, lockKey.Value())
	batch.Queue(createPartitionSql)

	results := d.Datastore.Pool.SendBatch(ctx, batch)
	if _, err := results.Exec(); err != nil { // SET LOCAL query
		results.Close()
		return err
	}
	if _, err := results.Exec(); err != nil { // pg_advisory_xact_lock query
		results.Close()
		return err
	}
	_, err = results.Exec() // createPartitionSql query
	closeErr := results.Close()
	if err != nil {
		// IF NOT EXISTS still races -- losing to a concurrent creator means it exists
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P07" {
			return nil
		}
		return err
	}
	return closeErr
}

// createPartitionAhead creates the partition after id's early, in the
// background. Best-effort: a failure warns and drops.
func (d *ProducerDatastore) createPartitionAhead(topicId int64, partitionSize int64, id int64) {
	next := (id/partitionSize + 1) * partitionSize

	go func() {
		// the produce ctx dies with its caller, so the run carries its own
		ctx, cancel := context.WithTimeout(context.Background(), d.createAheadTimeout)
		defer cancel()

		err := d.DatastoreRetry.Wrap(ctx, func() error {
			err := d.ensureCoveringPartition(ctx, topicId, partitionSize, next)
			if isLockNotAvailable(err) {
				return errPartitionLockTimeout.Wrap(err)
			}
			return err
		})
		if err != nil {
			// a missing parent table means the topic was destroyed while this
			// goroutine was in flight -- drop its claim entry
			if isMissingTable(err) {
				d.createAheadGate.delete(topicId)
				return
			}
			d.Logger.WarnContext(ctx, eventPartitionNotCreatedAhead.Message, "code", eventPartitionNotCreatedAhead.Code, "topic_id", topicId, "error", err)
		}
	}()
}

// ***************
// *** HELPERS ***
// ***************

// isMissingPartition matches an insert routed to a partition that doesn't exist yet.
func isMissingPartition(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23514" && // check_violation doubles as partition-routing failure
		strings.Contains(pgErr.Message, "no partition of relation")
}

// isMissingTable matches a statement against a table that no longer exists.
func isMissingTable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42P01" // undefined_table
}

// isLockNotAvailable matches a lock_timeout expiry.
func isLockNotAvailable(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "55P03" // lock_not_available
}
