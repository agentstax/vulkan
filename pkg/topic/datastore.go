package topic

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
	"github.com/jackc/pgx/v5"
)

type topicDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

func newTopicDatastore(datastore *datastore.PostgresDatastore, log logger.Logger, retryPolicy *retry.Policy) (*topicDatastore, error) {
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}

	dsRetry, err := retry.NewDatastoreRetry(retryPolicy, log)
	if err != nil {
		return nil, err
	}

	return &topicDatastore{
		Datastore: datastore,
		Retry:     dsRetry,
		Logger:    log,
	}, nil
}

func (d *topicDatastore) GetTopic(ctx context.Context, name string) (*Topic, error) {
	var topic *Topic
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		topic, err = d.getTopic(ctx, name)
		return err
	})
	return topic, err
}

func (d *topicDatastore) getTopic(ctx context.Context, name string) (*Topic, error) {
	sql := `
		SELECT
			id,
			name,
			partition_size,
			retention_ttl_ns,
			allow_drop_past_committed,
			idempotency_key_ttl_ns,
			disable_delivery_log,
			janitor_poll_rate_ns,
			janitor_sweep_batch_size
		FROM topic
		WHERE name = $1;
	`

	var t Topic
	var retentionTTLNs int64
	var idempotencyKeyTTLNs int64
	var janitorPollRateNs int64
	err := d.Datastore.Pool.QueryRow(ctx, sql, name).Scan(
		&t.Id,
		&t.Name,
		&t.PartitionSize,
		&retentionTTLNs,
		&t.AllowDropPastCommitted,
		&idempotencyKeyTTLNs,
		&t.DisableDeliveryLog,
		&janitorPollRateNs,
		&t.JanitorSweepBatchSize,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	t.RetentionTTL = time.Duration(retentionTTLNs)
	t.IdempotencyKeyTTL = time.Duration(idempotencyKeyTTLNs)
	t.JanitorPollRate = time.Duration(janitorPollRateNs)

	return &t, nil
}

// UpsertTopic resolves cfg.Name to its db identity, creating it if it doesn't exist.
func (d *topicDatastore) UpsertTopic(ctx context.Context, cfg Config) (*Topic, error) {
	var topic *Topic
	err := d.Retry.Wrap(ctx, func() error {
		var err error
		topic, err = d.upsertTopic(ctx, cfg)
		return err
	})
	return topic, err
}

func (d *topicDatastore) upsertTopic(ctx context.Context, cfg Config) (*Topic, error) {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	insertSql := `
		INSERT INTO topic (name, partition_size, retention_ttl_ns, allow_drop_past_committed, idempotency_key_ttl_ns, disable_delivery_log, janitor_poll_rate_ns, janitor_sweep_batch_size)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (name) DO NOTHING -- no rows are returned on conflict -> must GetTopic later
		RETURNING id;
	`

	var id int64
	insertErr := tx.QueryRow(ctx, insertSql, cfg.Name, cfg.PartitionSize, int64(cfg.RetentionTTL), cfg.AllowDropPastCommitted, int64(cfg.IdempotencyKeyTTL), cfg.DisableDeliveryLog, int64(cfg.JanitorPollRate), cfg.JanitorSweepBatchSize).Scan(&id)

	switch {
	case insertErr == nil:
		// we won the insert -- stand up this topic's own log
		if err := d.createTopicLog(ctx, tx, id, cfg.PartitionSize, cfg.DisableDeliveryLog); err != nil {
			return nil, err
		}
		d.Logger.InfoContext(ctx, "topic registered (created)", "topic", cfg.Name, "topic_id", id)
	case errors.Is(insertErr, pgx.ErrNoRows):
		// not writing here, so reading via the plain pool (not tx) is fine.
		// private getTopic, not GetTopic -- otherwise would have nested retries.
		found, err := d.getTopic(ctx, cfg.Name)
		if err != nil {
			return nil, err
		}
		if found == nil {
			return nil, fmt.Errorf("topic %s: lost the registration race and could not be resolved", cfg.Name)
		}
		id = found.Id
		if want := cfg.ToTopic(id); *found != *want {
			return nil, fmt.Errorf("%w: topic %s: existing=%+v got=%+v", ErrTopicConfigMismatch, cfg.Name, *found, *want)
		}
		d.Logger.InfoContext(ctx, "topic registered (already existed)", "topic", cfg.Name, "topic_id", id)
	default:
		return nil, insertErr
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return cfg.ToTopic(id), nil
}

// createTopicLog creates:
//
// - message_log_<id>
// - delivery_<id>
// - delivery_log_<id> (unless disableDeliveryLog)
//
// Split one per topic instead of shared because:
// - Drop Partition functionality -- DropExpiredPartitions/SweepExpiredPartitions only expire a
// partition once every cursor reading it has committed past it.
// Shared across topics, that cursor floor was computed over every topic's
// cursors at once, so one lagging consumer group on topic A blocked
// partition drops for a completely unrelated topic B riding along in the
// same table. Per-topic tables scope the floor to that topic's own cursors.
// - Per topic retention -- Identifying when to drop a partition requires
// looking at max(id) created_at > ttl for that partition. Under a shared topic table;
// topic 1 or topic 2 could be that max(id) and as such would drive when a
// partition is dropped ie would not have per topic retention it would be global retention.
// - Blast Radius -- If message processing has high failure rate (say in the event of an outage)
// delivery_<id> gets hit with a ton of churn (insert+delete). If shared, a topic with a high failure
// rate would bloat that singular shared table and slow down every OTHER topic's claim
// queries hitting the same physical disk pages. Per-topic contains that churn to
// the noisy topic alone.
// - Dense ID sequence -- A shared BIGSERIAL would leave each topic's ids scattered
// across a sparse subset of it, which breaks the head/partitionSize math
// EnsureNextPartition uses to create partitions when they are needed
func (d *topicDatastore) createTopicLog(ctx context.Context, tx pgx.Tx, id int64, partitionSize int64, disableDeliveryLog bool) error {
	createTableSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY, -- own sequence per table, so each topic's ids are independent
			routing_key TEXT,
			compaction_key TEXT,
			payload JSONB NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		) PARTITION BY RANGE (id);
	`, iTopic.MessageLogTable(id))
	if _, err := tx.Exec(ctx, createTableSql); err != nil {
		return err
	}

	// message_log_<id>_0 -- two-part name avoids colliding with another topic's table
	createPartitionSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s
			PARTITION OF %s
			FOR VALUES FROM (0) TO (%d);
	`, iTopic.MessageLogPartitionTable(id, 0), iTopic.MessageLogTable(id), partitionSize)
	if _, err := tx.Exec(ctx, createPartitionSql); err != nil {
		return err
	}

	// idempotency_key_<id> -- not partitioned (can't effectively be)
	createIdempotencyKeySql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			idempotency_key UUID NOT NULL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`, iTopic.IdempotencyKeyTable(id))
	if _, err := tx.Exec(ctx, createIdempotencyKeySql); err != nil {
		return err
	}

	// keeps the per-topic TTL sweep's cleanup DELETE an index scan instead
	// of a sequential scan
	createIdempotencyKeyCreatedAtIndexSql := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_created_at ON %s (created_at);
	`, iTopic.IdempotencyKeyTable(id), iTopic.IdempotencyKeyTable(id))
	if _, err := tx.Exec(ctx, createIdempotencyKeyCreatedAtIndexSql); err != nil {
		return err
	}

	createDeliverySql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group TEXT NOT NULL, -- PK
			message_id BIGINT NOT NULL,   -- PK
			status TEXT NOT NULL,
			attempts INT NOT NULL default 0,
			lease_until TIMESTAMPTZ,
			lease_token UUID,
			can_run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- backoff between retries
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (consumer_group, message_id)
		);
	`, iTopic.DeliveryTable(id))
	if _, err := tx.Exec(ctx, createDeliverySql); err != nil {
		return err
	}

	if disableDeliveryLog {
		return nil
	}

	createDeliveryLogSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group TEXT NOT NULL,        -- PK
			message_id BIGINT NOT NULL,          -- PK
			attempt INT NOT NULL,                -- PK
			attempted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			error TEXT NOT NULL,                 -- always populated -- a row only ever exists for a failed attempt
			PRIMARY KEY (consumer_group, message_id, attempt)
		);
	`, iTopic.DeliveryLogTable(id))
	_, err := tx.Exec(ctx, createDeliveryLogSql)
	return err
}

func (d *topicDatastore) DeleteTopic(ctx context.Context, topic *Topic) error {
	return d.Retry.Wrap(ctx, func() error {
		return d.deleteTopic(ctx, topic)
	})
}

func (d *topicDatastore) deleteTopic(ctx context.Context, topic *Topic) error {
	if err := d.drainPartitions(ctx, iTopic.MessageLogTable(topic.Id)); err != nil {
		if errors.Is(err, errPartitionsRemain) {
			return fmt.Errorf("topic %s: %w -- a producer is likely still writing; stop producers and call Destroy again", topic.Name, err)
		}
		return err
	}

	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM topic WHERE id = $1;`, topic.Id); err != nil {
		return err
	}

	// every other table scoped by topic_id
	for _, table := range []string{"cursor", "lease", "binding", "latest_key"} {
		deleteSql := fmt.Sprintf(`DELETE FROM %s WHERE topic_id = $1;`, table)
		if _, err := tx.Exec(ctx, deleteSql, topic.Id); err != nil {
			return err
		}
	}

	// the now-empty parent, delivery_<id>, and idempotency_key_<id>
	dropTableSql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, iTopic.MessageLogTable(topic.Id))
	if _, err := tx.Exec(ctx, dropTableSql); err != nil {
		return err
	}
	dropDeliverySql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, iTopic.DeliveryTable(topic.Id))
	if _, err := tx.Exec(ctx, dropDeliverySql); err != nil {
		return err
	}
	dropDeliveryLogSql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, iTopic.DeliveryLogTable(topic.Id))
	if _, err := tx.Exec(ctx, dropDeliveryLogSql); err != nil {
		return err
	}
	dropIdempotencyKeySql := fmt.Sprintf(`DROP TABLE IF EXISTS %s;`, iTopic.IdempotencyKeyTable(topic.Id))
	if _, err := tx.Exec(ctx, dropIdempotencyKeySql); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.WarnContext(ctx, "topic destroyed", "topic", topic.Name, "topic_id", topic.Id)
	return nil
}
