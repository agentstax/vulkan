package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
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

// ensureCoveringPartition creates the partition after head's, so the retry's
// fresh id has somewhere to land.
func (d *ProducerDatastore) ensureCoveringPartition(ctx context.Context, topicId int64, partitionSize int64) error {
	headSql := fmt.Sprintf(`
		-- vulkan: producer.ensureCoveringPartition
		SELECT COALESCE(MAX(id), 0) FROM %s;
	`, iTopic.MessageLogTable(topicId))

	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}

	next := head/partitionSize + 1

	createPartitionSql := fmt.Sprintf(`
		-- vulkan: producer.ensureCoveringPartition
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (%d) TO (%d);
	`, iTopic.MessageLogPartitionTable(topicId, next), iTopic.MessageLogTable(topicId), next*partitionSize, (next+1)*partitionSize)

	lockKey := advisoryLockKey(topicId, next)

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
	`, lockKey)
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
	_, err := results.Exec() // createPartitionSql query
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

// createPartitionAhead creates the next partition early, in the background.
// Best-effort: a failure warns and drops.
func (d *ProducerDatastore) createPartitionAhead(topicId int64, partitionSize int64) {
	go func() {
		// the produce ctx dies with its caller, so the run carries its own
		ctx, cancel := context.WithTimeout(context.Background(), d.createAheadTimeout)
		defer cancel()

		err := d.DatastoreRetry.Wrap(ctx, func() error {
			err := d.ensureCoveringPartition(ctx, topicId, partitionSize)
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

// advisoryLockKey packs the (topic, partition) pair into one bigint key,
// the two numbers sitting side by side in the int64's bits:
//
//	topicId<<20  slides topicId's bits 20 places left, leaving the low 20
//	             bits all zero -- same value as topicId * 2^20 (1048576)
//	| partition  bitwise OR copies partition's bits into those zeroed low
//	             bits -- same value as + partition, since the bits don't overlap
//
// e.g. topic 83, partition 4 -> 83*1048576 + 4 = 87031812, and no other
// (topic, partition) pair produces that number while partition stays under
// 2^20 (~1M). A partition past 2^20 bleeds into the next topic's key range.
func advisoryLockKey(topicId int64, partition int64) int64 {
	return topicId<<20 | partition
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
