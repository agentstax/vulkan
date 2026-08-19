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
// Per-topic tables instead of shared ones:
//   - partition drops wait on every cursor in the table -> one lagging group
//     on topic A would block drops for unrelated topic B
//   - retention reads each partition's age -> the youngest topic sharing a
//     partition would set every other topic's expiry
//   - a failure outage churns delivery_<id> with insert+delete -> shared, that
//     churn would slow every other topic's claim queries
//   - the create-ahead partition math needs ids dense from the topic's own
//     sequence -> a shared BIGSERIAL scatters them
func (d *TopicDatastore) createTopicTables(ctx context.Context, tx pgx.Tx, id int64, partitionSize int64) error {
	createTableSql := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			-- never ALTER SEQUENCE ... CACHE on this sequence: the consumer's
			-- claim fence assumes ids are issued in INSERT order, and a cached
			-- sequence hands out out-of-order id blocks

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

	// keeps a key's history read (compaction's ListKeyMessages) an index scan.
	// partial, so topics that never set a compaction key pay nothing.
	createCompactionKeyIndexSql := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s_compaction_key ON %s (compaction_key, id)
			WHERE compaction_key IS NOT NULL;
	`, iTopic.MessageLogTable(id), iTopic.MessageLogTable(id))
	if _, err := tx.Exec(ctx, createCompactionKeyIndexSql); err != nil {
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
			consumer_group_id BIGINT NOT NULL,                -- PK
			message_id BIGINT NOT NULL,                       -- PK
			status TEXT NOT NULL,                             -- 'ready' | 'processing' | 'inflight' | 'deferred' | 'done' | 'dead'
			attempts INT NOT NULL default 0,
			can_run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- backoff between retries
			last_error TEXT,
			lease_token UUID,
			lease_until TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (consumer_group_id, message_id)
		);
	`, iTopic.DeliveryTable(id))
	if _, err := tx.Exec(ctx, createDeliverySql); err != nil {
		return err
	}

	// delivery_log_<id> exists for every topic, whatever its delivery_log_mode.
	// One row per delivery event:
	//   - 'failure': the attempt ran and returned an error
	//   - 'superseded': dropped unrun -- a newer message on its compaction key exists
	//   - 'deferred': never ran -- another delivery held its compaction key
	//   - 'expired': a claim's lease ran out with no outcome recorded -- logged by the claim that takes the row over
	//   - 'killed': dead-lettered by the crash-loop backstop
	//   - 'success': the attempt ran clean -- written only under mode 'all'
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
