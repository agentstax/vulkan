package datastore

import (
	"context"
	"fmt"

	iTopic "github.com/agentstax/vulkan/internal/topic"
	"github.com/jackc/pgx/v5"
)

// createTopicTables creates the topic's own tables:
//
// - message_log_<id>
// - idempotency_key_<id>
// - delivery_<id>
// - delivery_log_<id>
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
func (d *TopicDatastore) createTopicTables(ctx context.Context, tx pgx.Tx, id int64, partitionSize int64) error {
	createTableSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY, -- own sequence per table, so each topic's ids are independent.
			-- Should never optimize cache sequence like:
			--   ALTER SEQUENCE table_name_id_seq CACHE 32
			-- the consumer's claim fence assumes ids are issued in INSERT order,
			-- and a cached sequence hands out-of-order id blocks

			routing_key TEXT,
			compaction_key TEXT,
			compaction_rank BIGINT NOT NULL DEFAULT 0,
			payload JSONB NOT NULL,
			options JSONB,                                -- sparse MessageOptions
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
			consumer_group_id BIGINT NOT NULL, -- PK
			message_id BIGINT NOT NULL,        -- PK
			status TEXT NOT NULL,
			attempts INT NOT NULL default 0,
			lease_until TIMESTAMPTZ,
			lease_token UUID,
			can_run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- backoff between retries
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (consumer_group_id, message_id)
		);
	`, iTopic.DeliveryTable(id))
	if _, err := tx.Exec(ctx, createDeliverySql); err != nil {
		return err
	}

	// delivery_log_<id> exists even when DisableDeliveryLog.
	// One row per delivery event that is not a success:
	//   - 'failure': the attempt ran and returned an error
	//   - 'superseded': dropped unrun -- a newer message on its compaction key exists
	//   - 'deferred': never ran -- another delivery held its compaction key
	//   - 'expired': a claim's lease ran out with no outcome recorded -- logged by the claim that takes the row over
	//   - 'killed': dead-lettered by the crash-loop backstop
	createDeliveryLogSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group_id BIGINT NOT NULL,    -- PK
			message_id BIGINT NOT NULL,           -- PK
			attempt INT NOT NULL,                 -- PK
			status TEXT NOT NULL DEFAULT 'failure',
			error TEXT NOT NULL,
			attempted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (consumer_group_id, message_id, attempt)
		);
	`, iTopic.DeliveryLogTable(id))
	_, err := tx.Exec(ctx, createDeliveryLogSql)
	return err
}
