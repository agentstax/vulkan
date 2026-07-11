package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type producerDatastore[Message any] struct {
	Datastore *coredatastore.PostgresDatastore
}

func NewProducerDatastore[Message any](ds *coredatastore.PostgresDatastore) *producerDatastore[Message] {
	return &producerDatastore[Message]{
		Datastore: ds,
	}
}

// logTable is topicID's own physical message log.
func logTable(topicID int64) string {
	return fmt.Sprintf("message_log_%d", topicID)
}

// partitionTable is logTable's nth partition -- message_log_<topic_id>_<n>.
func partitionTable(topicID, n int64) string {
	return fmt.Sprintf("%s_%d", logTable(topicID), n)
}

// AppendMessage self-heals a missing-partition insert: the janitor's
// create-ahead is the primary defense, this retry is the backstop for a burst
// that outran it. Rerunning producerFunc is safe because its writes all go
// through the tx that just rolled back -- that's the transactional-enqueue
// contract, not a new constraint.
func (d *producerDatastore[Message]) AppendMessage(ctx context.Context, topicID int64, partitionSize int64, producerFunc producer.ProducerFunc[Message], opts producer.ProduceOptions) (*Message, error) {
	message, err := d.appendMessage(ctx, topicID, producerFunc, opts)
	if err == nil || !isMissingPartition(err) {
		return message, err
	}

	if err := d.ensureCoveringPartition(ctx, topicID, partitionSize); err != nil {
		return nil, err
	}
	return d.appendMessage(ctx, topicID, producerFunc, opts)
}

// isMissingPartition matches an insert routed to a partition that doesn't exist yet.
func isMissingPartition(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) &&
		pgErr.Code == "23514" && // check_violation doubles as partition-routing failure
		strings.Contains(pgErr.Message, "no partition of relation")
}

// ensureCoveringPartition creates the partition after head's, so the retry's
// fresh id has somewhere to land. Headroom beyond that is the janitor's job.
func (d *producerDatastore[Message]) ensureCoveringPartition(ctx context.Context, topicID int64, partitionSize int64) error {
	headSql := fmt.Sprintf(`
		SELECT COALESCE(MAX(id), 0) FROM %s;
	`, logTable(topicID))

	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}

	next := head/partitionSize + 1

	createPartitionSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (%d) TO (%d);
	`, partitionTable(topicID, next), logTable(topicID), next*partitionSize, (next+1)*partitionSize)

	_, err := d.Datastore.Pool.Exec(ctx, createPartitionSql)
	if err != nil {
		// IF NOT EXISTS still races -- losing to a concurrent creator means it exists
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "42P07" {
			return nil
		}
		return err
	}

	return nil
}

func (d *producerDatastore[Message]) appendMessage(ctx context.Context, topicID int64, producerFunc producer.ProducerFunc[Message], opts producer.ProduceOptions) (*Message, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}

	// If Commit() is called successfully, Rollback() becomes a no-op and returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx)

	// let user do transactional enqueue and return work/message
	message, err := producerFunc(ctx, tx)
	if err != nil {
		return nil, err
	}

	sql := fmt.Sprintf(`
		INSERT INTO %s (payload, routing_key, compaction_key)
		VALUES (
			$1,
			NULLIF($2, ''), -- if routing_key is empty string '' insert as NULL
			NULLIF($3, '')  -- same convention for compaction_key
		)
		RETURNING id;
	`, logTable(topicID))

	var id int64
	if err := tx.QueryRow(ctx, sql, message, opts.RoutingKey, opts.CompactionKey).Scan(&id); err != nil {
		return nil, err
	}

	// zero write-amplification for unkeyed traffic -- the common case skips this entirely
	if opts.CompactionKey != "" {
		if err := d.upsertLatestKey(ctx, tx, topicID, opts.CompactionKey, id); err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}

	return message, nil
}

// upsertLatestKey keeps latest_keys pointed at the highest id seen for this compaction key
func (d *producerDatastore[Message]) upsertLatestKey(ctx context.Context, tx pgx.Tx, topicID int64, compactionKey string, id int64) error {
	sql := `
		INSERT INTO latest_keys (topic_id, compaction_key, latest_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (topic_id, compaction_key) DO UPDATE
		SET latest_id = EXCLUDED.latest_id
		WHERE latest_keys.latest_id < EXCLUDED.latest_id;
	`
	_, err := tx.Exec(ctx, sql, topicID, compactionKey, id)
	return err
}
