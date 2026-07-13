package producer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Datastore[Message any] interface {
	AppendMessage(ctx context.Context, topicID int64, partitionSize int64, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error)
}

type producerDatastore[Message any] struct {
	Datastore *coredatastore.PostgresDatastore
	Retry     *retry.DatastoreRetry // default Wrap classification covers everything except Commit -- classified inline at that call site
	Logger    logger.Logger
}

type ProducerDatastoreConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
}

func (c *ProducerDatastoreConfig) withDefaults() *ProducerDatastoreConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	return c
}

func NewProducerDatastore[Message any](ds *coredatastore.PostgresDatastore, cfg *ProducerDatastoreConfig) *producerDatastore[Message] {
	if cfg == nil {
		cfg = &ProducerDatastoreConfig{}
	}
	cfg.withDefaults()
	return &producerDatastore[Message]{
		Datastore: ds,
		Retry:     retry.NewDatastoreRetry(6, time.Second, 5*time.Minute, 2, cfg.Logger), // TODO - make this user config driven eventually
		Logger:    cfg.Logger,
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

// AppendMessage resolves the idempotency key once and never regenerates it on
// retry -- that's what makes a retried attempt safe after an ambiguous commit
// instead of a double-publish. SkipIdempotency leaves it uuid.Nil.
func (d *producerDatastore[Message]) AppendMessage(ctx context.Context, topicID int64, partitionSize int64, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	var idempotencyKey uuid.UUID
	if !opts.SkipIdempotency {
		idempotencyKey = opts.IdempotencyKey
		if idempotencyKey == uuid.Nil {
			var err error
			idempotencyKey, err = uuid.NewV7()
			if err != nil {
				return nil, err
			}
		}
	}

	var message *Message
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		message, err = d.appendMessageWithPartitionRetry(ctx, topicID, partitionSize, producerFunc, opts, idempotencyKey)
		return err
	})
	return message, err
}

// appendMessageWithPartitionRetry self-heals a missing-partition insert: the
// janitor's create-ahead is the primary defense, this retry is the backstop
// for a burst that outran it.
func (d *producerDatastore[Message]) appendMessageWithPartitionRetry(ctx context.Context, topicID int64, partitionSize int64, producerFunc ProducerFunc[Message], opts ProduceOptions, idempotencyKey uuid.UUID) (*Message, error) {
	message, err := d.appendMessage(ctx, topicID, producerFunc, opts, idempotencyKey)
	if err == nil || !isMissingPartition(err) {
		return message, err
	}

	d.Logger.WarnContext(ctx, "publish outran janitor create-ahead, self-healing missing partition", "topic_id", topicID)
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

func (d *producerDatastore[Message]) appendMessage(ctx context.Context, topicID int64, producerFunc ProducerFunc[Message], opts ProduceOptions, idempotencyKey uuid.UUID) (*Message, error) {
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

	// TODO - really don't like this insertUnprotected/Protected code need to think through if we can develop
	// some standard patterns that make it easier to follow while giving the same benefits of CTEs

	if opts.SkipIdempotency {
		if _, err := d.insertUnprotected(ctx, tx, topicID, message, opts); err != nil {
			return nil, err
		}
	} else {
		_, landed, err := d.insertProtected(ctx, tx, topicID, idempotencyKey, message, opts)
		if err != nil {
			return nil, err
		}
		if !landed {
			// claim already existed -- a retried call under the same key that's
			// already durable. Nothing new to commit, but the transaction we
			// opened above still needs closing.
			if err := tx.Commit(ctx); err != nil {
				return nil, err // nothing new was written -- safe for Retry to auto-classify
			}
			return message, nil
		}
	}

	// the one genuinely ambiguous point -- a blip AT Commit means we lost the
	// ack, not whether it landed.
	if err = tx.Commit(ctx); err != nil {
		if opts.SkipIdempotency {
			return nil, retry.NewPermanentError(err) // no idempotency_keys guard to catch a retried duplicate
		}
		return nil, err // idempotency_keys' ON CONFLICT DO NOTHING makes a retry safe
	}

	return message, nil
}

// insertUnprotected writes the message with no idempotency claim gate
func (d *producerDatastore[Message]) insertUnprotected(ctx context.Context, tx pgx.Tx, topicID int64, message *Message, opts ProduceOptions) (int64, error) {
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
		return 0, err
	}

	// zero write-amplification for unkeyed traffic
	if opts.CompactionKey != "" {
		if err := d.upsertLatestKey(ctx, tx, topicID, opts.CompactionKey, id); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// insertProtected runs the idempotency claim + message insert (+ latest_keys
// upsert when keyed) in one round trip. landed=false means the claim already
// existed -- WHERE EXISTS matched nothing, Scan comes back pgx.ErrNoRows.
func (d *producerDatastore[Message]) insertProtected(ctx context.Context, tx pgx.Tx, topicID int64, idempotencyKey uuid.UUID, message *Message, opts ProduceOptions) (id int64, landed bool, err error) {
	args := []any{topicID, idempotencyKey, message, opts.RoutingKey}

	var sql string
	if opts.CompactionKey != "" {
		// claim + insert + latest_keys upsert in one round trip -- inserted
		// stays empty when the claim already existed, so latest never fires either.
		sql = fmt.Sprintf(`
			WITH claim AS (
				INSERT INTO idempotency_keys (topic_id, idempotency_key)
				VALUES ($1, $2)
				ON CONFLICT (topic_id, idempotency_key) DO NOTHING
				RETURNING 1
			), inserted AS (
				INSERT INTO %s (payload, routing_key, compaction_key)
				SELECT $3, NULLIF($4, ''), $5 -- if routing_key $4 is empty string '' insert as NULL
				WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
				RETURNING id
			), latest AS (
				INSERT INTO latest_keys (topic_id, compaction_key, latest_id)
				SELECT $1, $5, id FROM inserted
				ON CONFLICT (topic_id, compaction_key) DO UPDATE
				SET latest_id = EXCLUDED.latest_id
				WHERE latest_keys.latest_id < EXCLUDED.latest_id
			)
			SELECT id FROM inserted;
		`, logTable(topicID))
		args = append(args, opts.CompactionKey)
	} else {
		// claim + insert in one round trip -- WHERE EXISTS only fires if the
		// claim CTE landed a row, so a conflict makes both match zero rows.
		sql = fmt.Sprintf(`
			WITH claim AS (
				INSERT INTO idempotency_keys (topic_id, idempotency_key)
				VALUES ($1, $2)
				ON CONFLICT (topic_id, idempotency_key) DO NOTHING
				RETURNING 1
			)
			INSERT INTO %s (payload, routing_key, compaction_key)
			SELECT
				$3,
				NULLIF($4, ''), -- if routing_key is empty string '' insert as NULL
				NULL
			WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
			RETURNING id;
		`, logTable(topicID))
	}

	err = tx.QueryRow(ctx, sql, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		d.Logger.DebugContext(ctx, "duplicate publish detected, idempotency claim already existed", "topic_id", topicID, "idempotency_key", idempotencyKey)
		return 0, false, nil // claim already existed -- already committed
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
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
