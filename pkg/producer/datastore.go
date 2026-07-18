package producer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/agentstax/vulkan/internal/topic"
	coredatastore "github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TODO - we need to split this file up
// TODO - this code needs a lot of work to read better it is currently jibberish

type Datastore[Message any] interface {
	AppendMessage(ctx context.Context, topicID int64, partitionSize int64, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error)
	AppendMessageInTx(ctx context.Context, tx pgx.Tx, topicID int64, partitionSize int64, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error)
}

type producerDatastore[Message any] struct {
	Datastore *coredatastore.PostgresDatastore
	Retry     *retry.DatastoreRetry // default Wrap classification covers everything except Commit -- classified inline at that call site
	Logger    logger.Logger
}

type ProducerDatastoreConfig struct {
	Logger logger.Logger // pass your own *slog.Logger (own Handler) or anything satisfying logger.Logger. Default: text logger to stdout, warn level and up.
	Retry  *retry.Policy // Default: retry.NewDefaultRetryPolicy().
}

func (c *ProducerDatastoreConfig) withDefaults() *ProducerDatastoreConfig {
	if c.Logger == nil {
		c.Logger = logger.NewDefaultLogger(os.Stdout)
	}
	c.Retry = c.Retry.WithDefaults()
	return c
}

func NewProducerDatastore[Message any](ds *coredatastore.PostgresDatastore, cfg *ProducerDatastoreConfig) *producerDatastore[Message] {
	if cfg == nil {
		cfg = &ProducerDatastoreConfig{}
	}
	cfg.withDefaults()
	return &producerDatastore[Message]{
		Datastore: ds,
		Retry:     retry.NewDatastoreRetry(cfg.Retry, cfg.Logger),
		Logger:    cfg.Logger,
	}
}

// AppendMessage resolves the idempotency key once and never regenerates it on
// retry -- that's what makes a retried attempt safe after an ambiguous commit
// instead of a double-publish.
func (d *producerDatastore[Message]) AppendMessage(ctx context.Context, topicID int64, partitionSize int64, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	idempotencyKey, err := resolveIdempotencyKey(opts)
	if err != nil {
		return nil, err
	}

	var message *Message
	err = d.Retry.Wrap(ctx, func() error {
		var err error
		message, err = d.appendMessageWithPartitionRetry(ctx, topicID, partitionSize, producerFunc, opts, idempotencyKey)
		return err
	})
	return message, err
}

// AppendMessageInTx runs producerFunc + the message insert against a
// caller-supplied tx -- no Begin/Commit/Rollback, that's owned by whoever
// opened tx (producer.InTransaction). Self-heals a missing partition inside
// its own SAVEPOINT (runInsertSavepoint), so retrying here can't undo an
// earlier target's insert or rerun a caller side effect between calls.
func (d *producerDatastore[Message]) AppendMessageInTx(ctx context.Context, tx pgx.Tx, topicID int64, partitionSize int64, producerFunc ProducerFunc[Message], opts ProduceOptions) (*Message, error) {
	idempotencyKey, err := resolveIdempotencyKey(opts)
	if err != nil {
		return nil, err
	}

	message, err := d.runInsertSavepoint(ctx, tx, topicID, producerFunc, opts, idempotencyKey)
	if err == nil || !isMissingPartition(err) {
		return message, err
	}

	d.Logger.WarnContext(ctx, "publish outran janitor create-ahead, self-healing missing partition", "topic_id", topicID)
	if err := d.ensureCoveringPartition(ctx, topicID, partitionSize); err != nil {
		return nil, err
	}
	message, err = d.runInsertSavepoint(ctx, tx, topicID, producerFunc, opts, idempotencyKey)
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
	`, topic.MessageLogTable(topicID))

	var head int64
	if err := d.Datastore.Pool.QueryRow(ctx, headSql).Scan(&head); err != nil {
		return err
	}

	next := head/partitionSize + 1

	createPartitionSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (%d) TO (%d);
	`, topic.PartitionTable(topicID, next), topic.MessageLogTable(topicID), next*partitionSize, (next+1)*partitionSize)

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

	message, landed, err := d.runInsert(ctx, tx, topicID, producerFunc, opts, idempotencyKey)
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

	// the one genuinely ambiguous point -- a blip AT Commit means we lost the
	// ack, not whether it landed. idempotency_key's ON CONFLICT DO NOTHING
	// makes a retry safe.
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}

	return message, nil
}

// runInsert runs producerFunc + the protected message insert against an already-open tx
func (d *producerDatastore[Message]) runInsert(ctx context.Context, tx pgx.Tx, topicID int64, producerFunc ProducerFunc[Message], opts ProduceOptions, idempotencyKey uuid.UUID) (message *Message, landed bool, err error) {
	// let user do transactional enqueue and return work/message
	message, err = producerFunc(ctx, newVulkanTx(tx), idempotencyKey)
	if err != nil {
		return nil, false, err
	}

	_, landed, err = d.insertProtected(ctx, tx, topicID, idempotencyKey, message, opts)
	if err != nil {
		return nil, false, err
	}
	return message, landed, nil
}

// produceInTxSavepoint is a fixed name, not per-call unique -- safe because
// calls only ever run sequentially against one tx (pgx.Tx isn't safe for
// concurrent use), so each use is released before the next begins.
const produceInTxSavepoint = "sp_produce_in_tx"

// runInsertSavepoint wraps producerFunc + the message insert in a SAVEPOINT
// scoped to just this call, so a missing-partition retry can't touch
// anything else already done in tx.
func (d *producerDatastore[Message]) runInsertSavepoint(ctx context.Context, tx pgx.Tx, topicID int64, producerFunc ProducerFunc[Message], opts ProduceOptions, idempotencyKey uuid.UUID) (message *Message, err error) {
	if err := commitToSavepoint(ctx, tx, produceInTxSavepoint); err != nil {
		return nil, err
	}

	message, err = producerFunc(ctx, newVulkanTx(tx), idempotencyKey)
	if err != nil {
		attemptRollbackToSavepoint(ctx, tx, produceInTxSavepoint)
		return nil, err
	}

	err = d.insertProtectedSavepoint(ctx, tx, topicID, idempotencyKey, message, opts)
	if err != nil {
		attemptRollbackToSavepoint(ctx, tx, produceInTxSavepoint)
		return nil, err
	}
	return message, nil
}

func commitToSavepoint(ctx context.Context, tx pgx.Tx, savepointName string) error {
	_, err := tx.Exec(ctx, "SAVEPOINT "+savepointName+";")
	return err
}

func attemptRollbackToSavepoint(ctx context.Context, tx pgx.Tx, savepointName string) {
	_, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+savepointName+";")
}

// insertProtectedSavepoint pipelines the claim+insert CTE with RELEASE
// SAVEPOINT as one round trip -- always a single statement regardless of
// CompactionKey, so it always fully batches.
func (d *producerDatastore[Message]) insertProtectedSavepoint(ctx context.Context, tx pgx.Tx, topicID int64, idempotencyKey uuid.UUID, message *Message, opts ProduceOptions) error {
	sql, args := protectedInsertSQL(topicID, idempotencyKey, message, opts)

	batch := &pgx.Batch{}
	batch.Queue(sql, args...)
	batch.Queue("RELEASE SAVEPOINT " + produceInTxSavepoint + ";")

	br := tx.SendBatch(ctx, batch)
	var id int64
	err := br.QueryRow().Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		// claim already existed -- inserted CTE never ran. Not a failure:
		// RELEASE SAVEPOINT is still queued next and still needs reading.
		d.Logger.DebugContext(ctx, "duplicate publish detected, idempotency claim already existed", "topic_id", topicID, "idempotency_key", idempotencyKey)
	} else if err != nil {
		br.Close()
		return err
	}

	if _, err := br.Exec(); err != nil { // RELEASE SAVEPOINT
		br.Close()
		return err
	}
	return br.Close()
}

// insertProtected runs the idempotency claim + message insert (+ latest_key
// upsert when keyed) in one round trip. landed=false means the claim already
// existed -- WHERE EXISTS matched nothing, Scan comes back pgx.ErrNoRows.
func (d *producerDatastore[Message]) insertProtected(ctx context.Context, tx pgx.Tx, topicID int64, idempotencyKey uuid.UUID, message *Message, opts ProduceOptions) (id int64, landed bool, err error) {
	sql, args := protectedInsertSQL(topicID, idempotencyKey, message, opts)

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

// protectedInsertSQL builds the claim+insert(+latest_key upsert when keyed)
// CTE -- shared with the savepoint-batched path so both run the exact same
// statement.
func protectedInsertSQL(topicID int64, idempotencyKey uuid.UUID, message any, opts ProduceOptions) (string, []any) {
	args := []any{topicID, idempotencyKey, message, opts.RoutingKey}

	var sql string
	if opts.CompactionKey != "" {
		// claim + insert + latest_key upsert in one round trip -- inserted
		// stays empty when the claim already existed, so latest never fires either.
		sql = fmt.Sprintf(`
			WITH claim AS (
				INSERT INTO idempotency_key (topic_id, idempotency_key)
				VALUES ($1, $2)
				ON CONFLICT (topic_id, idempotency_key) DO NOTHING
				RETURNING 1
			), inserted AS (
				INSERT INTO %s (payload, routing_key, compaction_key, idempotency_key)
				SELECT $3, NULLIF($4, ''), $5, $2 -- if routing_key $4 is empty string '' insert as NULL
				WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
				RETURNING id
			), latest AS (
				INSERT INTO latest_key (topic_id, compaction_key, latest_id)
				SELECT $1, $5, id FROM inserted
				ON CONFLICT (topic_id, compaction_key) DO UPDATE
				SET latest_id = EXCLUDED.latest_id
				WHERE latest_key.latest_id < EXCLUDED.latest_id
			)
			SELECT id FROM inserted;
		`, topic.MessageLogTable(topicID))
		args = append(args, opts.CompactionKey)
	} else {
		// claim + insert in one round trip -- WHERE EXISTS only fires if the
		// claim CTE landed a row, so a conflict makes both match zero rows.
		sql = fmt.Sprintf(`
			WITH claim AS (
				INSERT INTO idempotency_key (topic_id, idempotency_key)
				VALUES ($1, $2)
				ON CONFLICT (topic_id, idempotency_key) DO NOTHING
				RETURNING 1
			)
			INSERT INTO %s (payload, routing_key, compaction_key, idempotency_key)
			SELECT
				$3,
				NULLIF($4, ''), -- if routing_key is empty string '' insert as NULL
				NULL,
				$2
			WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
			RETURNING id;
		`, topic.MessageLogTable(topicID))
	}

	return sql, args
}

// resolveIdempotencyKey generates a fresh UUIDv7 unless the caller supplied one
func resolveIdempotencyKey(opts ProduceOptions) (uuid.UUID, error) {
	if opts.IdempotencyKey != uuid.Nil {
		return opts.IdempotencyKey, nil
	}
	return uuid.NewV7()
}
