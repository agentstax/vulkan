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
// - exception_queue_<id>
// - delivery_log_<id>
// - consumer_group_cursor_<id>
// - claim_lease_<id>
// - key_lease_<id>
// - compaction_head_<id>
// - binding_config_<id>
// - binding_config_log_<id>
//
// Per-topic tables instead of shared ones:
//   - partition drops wait on every cursor in the table -> one lagging group
//     on topic A would block drops for unrelated topic B
//   - retention reads each partition's age -> the youngest topic sharing a
//     partition would set every other topic's expiry
//   - a failure outage churns exception_queue_<id> with insert+delete ->
//     shared, that churn would slow every other topic's claim queries
//   - the create-ahead partition math needs ids dense from the topic's own
//     sequence -> a shared BIGSERIAL scatters them
func (d *TopicDatastore) createTopicTables(ctx context.Context, tx pgx.Tx, id int64, partitionSize int64) error {
	createTableSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
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
		-- vulkan: topic.createTopicTables
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
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %s_compaction_key ON %s (compaction_key, id)
			WHERE compaction_key IS NOT NULL;
	`, iTopic.MessageLogTable(id), iTopic.MessageLogTable(id))
	if _, err := tx.Exec(ctx, createCompactionKeyIndexSql); err != nil {
		return err
	}

	// idempotency_key_<id> -- not partitioned (can't effectively be)
	createIdempotencyKeySql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
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
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %s_created_at ON %s (created_at);
	`, iTopic.IdempotencyKeyTable(id), iTopic.IdempotencyKeyTable(id))
	if _, err := tx.Exec(ctx, createIdempotencyKeyCreatedAtIndexSql); err != nil {
		return err
	}

	createExceptionQueueSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group_id BIGINT NOT NULL,                -- PK
			message_id BIGINT NOT NULL,                       -- PK
			status TEXT NOT NULL,                             -- 'ready' | 'processing' | 'inflight' | 'deferred' | 'done' | 'dead'
			attempts INT NOT NULL default 0,
			can_run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- backoff between retries
			last_error TEXT,
			lease_token UUID,
			lease_expires_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (consumer_group_id, message_id)
		);
	`, iTopic.ExceptionQueueTable(id))
	if _, err := tx.Exec(ctx, createExceptionQueueSql); err != nil {
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
		-- vulkan: topic.createTopicTables
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
	if _, err := tx.Exec(ctx, createDeliveryLogSql); err != nil {
		return err
	}

	// consumer group cursors for tracking position in message_log_<id>.
	// UNIQUE keeps group <-> cursor 1:1
	createConsumerGroupCursorSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL UNIQUE REFERENCES consumer_group_config (id) ON DELETE CASCADE,
			claimed BIGINT NOT NULL DEFAULT 0,      -- the read frontier 'inflight' work
			committed BIGINT NOT NULL DEFAULT 0,    -- every message id > committed is in an end state done / dead
			-- the snapshot fence: claims stop at settled_head, not the raw MAX(id),
			-- MAX(id) can sit above uncommitted lower ids -- see FreshClaimMessagesWithCursor
			settled_head BIGINT NOT NULL DEFAULT 0, -- highest id proven to have nothing uncommitted at or below it
			pending_head BIGINT NOT NULL DEFAULT 0, -- candidate head awaiting that proof
			pending_xmax XID8                       -- txid fence read in the same snapshot as pending_head
		);
	`, iTopic.ConsumerGroupCursorTable(id))
	if _, err := tx.Exec(ctx, createConsumerGroupCursorSql); err != nil {
		return err
	}

	createClaimLeaseSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			token UUID NOT NULL DEFAULT gen_random_uuid(),
			consumer_group_id BIGINT NOT NULL,
			low BIGINT NOT NULL,             -- low of claimed range of lease
			high BIGINT NOT NULL,            -- high of claimed range of lease
			expires_at TIMESTAMPTZ NOT NULL, -- past it the lease is reclaimed
			reclaims INT NOT NULL DEFAULT 0, -- times this range has been reclaimed; past MaxReclaims it's quarantined
			PRIMARY KEY (token, consumer_group_id)
		);
	`, iTopic.ClaimLeaseTable(id))
	if _, err := tx.Exec(ctx, createClaimLeaseSql); err != nil {
		return err
	}

	// key_lease_<id>: at most one in-flight delivery per compaction key per
	// consumer group.
	createKeyLeaseSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group_id BIGINT NOT NULL, -- PK
			compaction_key TEXT NOT NULL,      -- PK
			lease_token UUID NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (consumer_group_id, compaction_key)
		);
	`, iTopic.KeyLeaseTable(id))
	if _, err := tx.Exec(ctx, createKeyLeaseSql); err != nil {
		return err
	}

	// compaction_head_<id>: O(1) index for compaction's "is this the winner
	// for its key" lookup -- upserted synchronously in the same transaction
	// as every keyed publish, never a background job.
	createCompactionHeadSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			compaction_key  TEXT   NOT NULL PRIMARY KEY,
			head_id         BIGINT NOT NULL,           -- the winning message_log id for this key
			compaction_rank BIGINT NOT NULL DEFAULT 0  -- the winner's rank
		);
	`, iTopic.CompactionHeadTable(id))
	if _, err := tx.Exec(ctx, createCompactionHeadSql); err != nil {
		return err
	}

	// bindings: routing rules. A group with no binding matches all messages; a
	// group WITH a binding only receives messages whose routing_key matches
	// `pattern_regex`.
	createBindingConfigSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL REFERENCES consumer_group_config (id) ON DELETE CASCADE,
			pattern_regex TEXT NOT NULL,              -- POSIX regex translated from the declared pattern
			pattern TEXT,                             -- the declared NATS-style pattern, for humans
			UNIQUE (consumer_group_id, pattern_regex) -- its index also serves the group lookup
		);
	`, iTopic.BindingConfigTable(id))
	if _, err := tx.Exec(ctx, createBindingConfigSql); err != nil {
		return err
	}

	// binding_config_log_<id>: one row appended per declaration attempt, never
	// updated or deleted.
	// newest installed row per group -> the effective set's declaration
	// newest waiting row per declarer -> its latest still-blocked retry
	// Claims never read this table; the effective set stays in binding_config rows.
	createBindingConfigLogSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL REFERENCES consumer_group_config (id) ON DELETE CASCADE,
			status TEXT NOT NULL,                            -- 'installed' | 'waiting'
			patterns TEXT[] NOT NULL,                        -- the full declared set, original NATS-style; empty = whole topic
			declared_by TEXT NOT NULL,                       -- hostname:pid:<random> of the declaring process, display only
			declared_at TIMESTAMPTZ NOT NULL,                -- when this declarer first stated this set; constant across its retries
			attempted_at TIMESTAMPTZ NOT NULL DEFAULT now()  -- when this attempt ran; an installed row's declared_at -> attempted_at is the wait it ended
		);
	`, iTopic.BindingConfigLogTable(id))
	if _, err := tx.Exec(ctx, createBindingConfigLogSql); err != nil {
		return err
	}

	// helps listDeclarations so it doesn't have to sequential
	// scan a long wait's appended retry rows
	createBindingLogIndexSql := fmt.Sprintf(`
		-- vulkan: topic.createTopicTables
		CREATE INDEX IF NOT EXISTS %s_group ON %s (consumer_group_id, status, declared_by, id);
	`, iTopic.BindingConfigLogTable(id), iTopic.BindingConfigLogTable(id))
	_, err := tx.Exec(ctx, createBindingLogIndexSql)
	return err
}
