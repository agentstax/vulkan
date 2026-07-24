package system

import (
	"context"
	"os"

	"github.com/agentstax/vulkan/pkg/common"
	"github.com/agentstax/vulkan/pkg/datastore"
	"github.com/agentstax/vulkan/pkg/logger"
	"github.com/agentstax/vulkan/pkg/retry"
)

// SystemDatastore owns the shared control-plane schema.
// Tables:
// - cursor
// - lease
// - binding
// - topic
// - latest_key
// - schema_log
type SystemDatastore struct {
	Datastore *datastore.PostgresDatastore
	Retry     *retry.DatastoreRetry
	Logger    logger.Logger
}

func NewSystemDatastore(ds *datastore.PostgresDatastore, retryPolicy *retry.Policy, log logger.Logger) (*SystemDatastore, error) {
	if log == nil {
		log = logger.NewDefaultLogger(os.Stdout)
	}

	dsRetry, err := retry.NewDatastoreRetry(retryPolicy, log)
	if err != nil {
		return nil, err
	}

	return &SystemDatastore{
		Datastore: ds,
		Retry:     dsRetry,
		Logger:    log,
	}, nil
}

func (d *SystemDatastore) RegisterSystem(ctx context.Context) error {
	return d.Retry.Wrap(ctx, func() error {
		return d.registerSystem(ctx)
	})
}

// registerSystem creates the shared control-plane schema. Every statement is
// CREATE IF NOT EXISTS -- a no-op against a database that already has the
// tables, a full bootstrap against a fresh one.
//
// This is the BASELINE, after v1 shipped changes to system should be done
// via migration steps.
func (d *SystemDatastore) registerSystem(ctx context.Context) error {
	tx, err := d.Datastore.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// txn-scoped -- acquired here, auto-released at commit.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1);`, common.AdvisoryLock); err != nil {
		return err
	}

	// consumer group cursors for tracking offset in message_log
	createCursorSql := `
		CREATE TABLE IF NOT EXISTS cursor (
			consumer_group TEXT NOT NULL,
			topic_id BIGINT NOT NULL,               -- a group tracks an independent cursor per topic
			claimed BIGINT NOT NULL DEFAULT 0,      -- the read frontier 'inflight' work
			committed BIGINT NOT NULL DEFAULT 0,    -- every message id > committed is in an end state done / dead
			-- the snapshot fence: claims stop at settled_head, not the raw MAX(id),
			-- MAX(id) can sit above uncommitted lower ids -- see FreshClaimMessagesWithCursor
			settled_head BIGINT NOT NULL DEFAULT 0, -- highest id proven to have nothing uncommitted at or below it
			pending_head BIGINT NOT NULL DEFAULT 0, -- candidate head awaiting that proof
			pending_xmax XID8,                      -- txid fence read in the same snapshot as pending_head
			PRIMARY KEY (consumer_group, topic_id)
		);
	`
	if _, err := tx.Exec(ctx, createCursorSql); err != nil {
		return err
	}

	createLeaseSql := `
		CREATE TABLE IF NOT EXISTS lease (
			token UUID NOT NULL DEFAULT gen_random_uuid(),
			consumer_group TEXT NOT NULL,
			topic_id BIGINT NOT NULL,        -- this is for range interpretation (which message_log_<id>)
			low BIGINT NOT NULL,             -- low of claimed range of lease
			high BIGINT NOT NULL,            -- high of claimed range of lease
			until TIMESTAMPTZ NOT NULL,      -- when the lease is considered expired and should be reclaimed
			reclaims INT NOT NULL DEFAULT 0, -- times this range has been reclaimed; past MaxReclaims it's quarantined
			PRIMARY KEY (token, consumer_group)
		);
	`
	if _, err := tx.Exec(ctx, createLeaseSql); err != nil {
		return err
	}

	// bindings: routing rules. A group with no binding matches all events; a
	// group WITH a binding only receives events whose routing_key matches
	// `pattern`.
	createBindingSql := `
		CREATE TABLE IF NOT EXISTS binding (
			id BIGSERIAL PRIMARY KEY,
			consumer_group TEXT NOT NULL,
			topic_id BIGINT NOT NULL,
			pattern TEXT NOT NULL,   -- POSIX regex translated from the NATS-style pattern
			display TEXT             -- original NATS pattern, for humans
		);
	`
	if _, err := tx.Exec(ctx, createBindingSql); err != nil {
		return err
	}
	createBindingIndexSql := `CREATE INDEX IF NOT EXISTS binding_group ON binding (consumer_group, topic_id);`
	if _, err := tx.Exec(ctx, createBindingIndexSql); err != nil {
		return err
	}

	createTopicSql := `
		CREATE TABLE IF NOT EXISTS topic (
			id BIGSERIAL PRIMARY KEY,                                      -- corresponding id for table interpolation ie message_log_<id>
			name TEXT UNIQUE NOT NULL,                                     -- user defined and displayed name
			partition_size BIGINT NOT NULL,                                -- immutable after creation; message_log_<id>'s partition boundaries depend on it staying fixed
			retention_ttl_ns BIGINT NOT NULL DEFAULT 0,                    -- nanoseconds, time.Duration's own unit; 0 disables retention
			allow_drop_past_committed BOOLEAN NOT NULL DEFAULT false,      -- opt into Kafka's "lagging consumer falls off the retention window" semantics
			idempotency_key_ttl_ns BIGINT NOT NULL DEFAULT 86400000000000, -- nanoseconds; unlike retention_ttl_ns, 0 isn't a supported "keep forever" value -- Config.SetDefaults never lets it reach 0, so the column default is the real 24h value, not 0
			disable_delivery_log BOOLEAN NOT NULL DEFAULT false,           -- opt out of delivery_log_<id> (per-attempt failure audit trail)
			janitor_poll_rate_ns BIGINT NOT NULL DEFAULT 5000000000,       -- nanoseconds; how often the janitor loop ticks (create-ahead, drop/sweep expired partitions, sweep idempotency_key)
			janitor_sweep_batch_size INT NOT NULL DEFAULT 1000,            -- rows deleted per sweep transaction; caps how much of a backlog one batch holds a lock for
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	if _, err := tx.Exec(ctx, createTopicSql); err != nil {
		return err
	}

	// O(1) index for compaction's "is this the latest for its key" lookup --
	// upserted synchronously in the same transaction as every keyed publish,
	// never a background job. Shared across topics (not per-topic like
	// message_log) since it scales with DISTINCT compaction_key count, not
	// total message volume.
	createLatestKeySql := `
		CREATE TABLE IF NOT EXISTS latest_key (
			topic_id       BIGINT NOT NULL, -- PK
			compaction_key TEXT   NOT NULL, -- PK
			latest_id      BIGINT NOT NULL, -- highest message_log id seen for this key so far
			PRIMARY KEY (topic_id, compaction_key)
		);
	`
	if _, err := tx.Exec(ctx, createLatestKeySql); err != nil {
		return err
	}

	// schema_log is the append-only history of schema-version changes
	// -- one row per attempt. entity_type + entity_id say whose:
	// ('system', 0) for the control plane, ('topic', topic_id) per topic.
	createSchemaLogSql := `
		CREATE TABLE IF NOT EXISTS schema_log (
			id BIGSERIAL PRIMARY KEY,
			entity_type TEXT NOT NULL,    -- 'system' | 'topic'
			entity_id BIGINT NOT NULL,    -- 0 for system; topic_id for a topic
			schema_version BIGINT NOT NULL,
			status TEXT NOT NULL,         -- 'success' | 'failure' (extensible)
			error TEXT,                   -- populated when status = 'failure'
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	if _, err := tx.Exec(ctx, createSchemaLogSql); err != nil {
		return err
	}

	// Record the system v1 baseline, but only if there's no success row yet
	recordBaselineSql := `
		INSERT INTO schema_log (entity_type, entity_id, schema_version, status)
		SELECT 'system', 0, 1, 'success'
		WHERE NOT EXISTS (
			SELECT 1 FROM schema_log 
			WHERE entity_type = 'system' 
				AND entity_id = 0 
				AND status = 'success'
		);
	`
	if _, err := tx.Exec(ctx, recordBaselineSql); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	d.Logger.InfoContext(ctx, "system schema registered")
	return nil
}
