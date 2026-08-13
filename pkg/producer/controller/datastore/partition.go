package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ddlLockTimeout caps how long the self-heal CREATE waits for its table lock.
// WAITING is the hazard, not failing: postgres queues every later produce and
// claim behind the lock, so a stuck lock holder would stall the whole topic --
// better to fail this produce fast and let the caller's retry policy own it.
const ddlLockTimeout = 2 * time.Second

// isMissingPartition matches an insert routed to a partition that doesn't exist yet.
func isMissingPartition(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23514" && // check_violation doubles as partition-routing failure
		strings.Contains(pgErr.Message, "no partition of relation")
}

// ensureCoveringPartition creates the partition after head's, so the retry's
// fresh id has somewhere to land.
func (d *ProducerDatastore[Message]) ensureCoveringPartition(ctx context.Context, topicId int64, partitionSize int64) error {
	headSql := fmt.Sprintf(`
		SELECT COALESCE(MAX(id), 0) FROM %s;
	`, topic.MessageLogTable(topicId))

	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}

	next := head/partitionSize + 1

	createPartitionSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (%d) TO (%d);
	`, topic.MessageLogPartitionTable(topicId, next), topic.MessageLogTable(topicId), next*partitionSize, (next+1)*partitionSize)

	// one round trip -- a batch outside an explicit txn runs as one implicit
	// transaction, which scopes the SET LOCAL to exactly these two statements
	// instead of leaking it to whatever might use this pooled connection next
	batch := &pgx.Batch{}
	batch.Queue(fmt.Sprintf(`SET LOCAL lock_timeout = '%dms';`, ddlLockTimeout.Milliseconds()))
	batch.Queue(createPartitionSql)

	results := d.Datastore.Pool.SendBatch(ctx, batch)
	if _, err := results.Exec(); err != nil {
		results.Close()
		return err
	}
	_, err := results.Exec()
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
