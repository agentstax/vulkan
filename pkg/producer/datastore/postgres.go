package datastore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/producer"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type producerDatastore[Message any] struct {
	Datastore *coredatastore.PostgresDatastore
	DBRetry   *retry.DatastoreRetry // shared across every exported method's DB round-trips -- cushions transient blips without masking a real outage
}

func NewProducerDatastore[Message any](ds *coredatastore.PostgresDatastore) *producerDatastore[Message] {
	return &producerDatastore[Message]{
		Datastore: ds,
		DBRetry:   retry.NewDatastoreRetry(6, time.Second, 5*time.Minute, 2), // TODO - make this user config driven eventually
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

// AppendMessage resolves the idempotency key once -- the caller's own if they
// supplied one (protects across their own retries, e.g. a process restart),
// otherwise a fresh one generated here (protects only against the retries
// DBRetry itself performs below). Never regenerated on retry: that's what
// makes a retried appendMessageWithPartitionRetry -- including its re-run of
// producerFunc -- safe after an ambiguous commit instead of a double-publish.
func (d *producerDatastore[Message]) AppendMessage(ctx context.Context, topicID int64, partitionSize int64, producerFunc producer.ProducerFunc[Message], opts producer.ProduceOptions) (*Message, error) {
	idempotencyKey := opts.IdempotencyKey
	if idempotencyKey == uuid.Nil {
		var err error
		idempotencyKey, err = uuid.NewV7()
		if err != nil {
			return nil, err
		}
	}

	var message *Message
	err := d.DBRetry.Wrap(ctx, func() error {
		var err error
		message, err = d.appendMessageWithPartitionRetry(ctx, topicID, partitionSize, producerFunc, opts, idempotencyKey)
		return err
	})
	return message, err
}

// appendMessageWithPartitionRetry self-heals a missing-partition insert: the
// janitor's create-ahead is the primary defense, this retry is the backstop
// for a burst that outran it.
func (d *producerDatastore[Message]) appendMessageWithPartitionRetry(ctx context.Context, topicID int64, partitionSize int64, producerFunc producer.ProducerFunc[Message], opts producer.ProduceOptions, idempotencyKey uuid.UUID) (*Message, error) {
	message, err := d.appendMessage(ctx, topicID, producerFunc, opts, idempotencyKey)
	if err == nil || !isMissingPartition(err) {
		return message, err
	}

	if err := d.ensureCoveringPartition(ctx, topicID, partitionSize); err != nil {
		return nil, err
	}
	// Rerunning producerFunc is safe because its
	// writes all go through the tx that just rolled back
	return d.appendMessage(ctx, topicID, producerFunc, opts, idempotencyKey)
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

func (d *producerDatastore[Message]) appendMessage(ctx context.Context, topicID int64, producerFunc producer.ProducerFunc[Message], opts producer.ProduceOptions, idempotencyKey uuid.UUID) (*Message, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}

	// If Commit() is called successfully, Rollback() becomes a no-op and returns pgx.ErrTxClosed.
	defer tx.Rollback(ctx)

	// let user do transactional enqueue and return work/message
	message, err := producerFunc(ctx, tx, idempotencyKey)
	if err != nil {
		return nil, err
	}

	// claim the idempotency key BEFORE inserting the message
	claimSql := `
		INSERT INTO idempotency_keys (topic_id, idempotency_key)
		VALUES ($1, $2)
		ON CONFLICT (topic_id, idempotency_key) DO NOTHING;
	`
	tag, err := tx.Exec(ctx, claimSql, topicID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	// 0 rows affected -> the INSERT no-op'd on conflict -> idempotency_key already committed
	if tag.RowsAffected() == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		return message, nil
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
