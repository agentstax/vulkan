package datastore

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// createSystemTables creates the shared control-plane schema every topic rides
// on. This is the BASELINE -- later schema changes go through migration steps,
// not edits here.
//
// Every statement is CREATE IF NOT EXISTS: a no-op against a database that
// already has the tables, a full bootstrap against a fresh one. Table creation
// needs no system row -- a FK target only has to exist, so the whole schema
// lands before registerSystem seeds anything into it.
func (d *SystemDatastore) createSystemTables(ctx context.Context, tx pgx.Tx) error {
	createSystemSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS system (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	if _, err := tx.Exec(ctx, createSystemSql); err != nil {
		return err
	}

	createTopicSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS topic (
			id BIGSERIAL PRIMARY KEY,                                           -- corresponding id for table interpolation ie message_log_<id>
			system_id BIGINT NOT NULL REFERENCES system (id) ON DELETE CASCADE, -- owning system
			name TEXT NOT NULL,                                                 -- user defined and displayed name
			schema_version BIGINT NOT NULL,                                     -- a version bump is a whole new topic row; unrelated to migration_log.migration_version below (the DB-migration axis)
			partition_size BIGINT NOT NULL,                                     -- immutable after creation; message_log_<id>'s partition boundaries depend on it staying fixed
			retention_ttl_ns BIGINT NOT NULL DEFAULT 0,                         -- nanoseconds, time.Duration's own unit; 0 disables retention
			allow_drop_past_committed BOOLEAN NOT NULL DEFAULT false,           -- opt into Kafka's "lagging consumer falls off the retention window" semantics
			idempotency_key_ttl_ns BIGINT NOT NULL DEFAULT 86400000000000,      -- nanoseconds; unlike retention_ttl_ns, 0 isn't a supported "keep forever" value -- Config.SetDefaults never lets it reach 0, so the column default is the real 24h value, not 0
			delivery_log_mode TEXT NOT NULL DEFAULT 'failures',                 -- which outcomes write delivery_log_<id> rows
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (name, schema_version)
		);
	`
	if _, err := tx.Exec(ctx, createTopicSql); err != nil {
		return err
	}

	// consumer_group table provides:
	// - lifcycle management for child ownershipt model (cursor, binding, maintainence)
	createConsumerGroupSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS consumer_group (
			id BIGSERIAL PRIMARY KEY,                                         -- what children reference
			topic_id BIGINT NOT NULL REFERENCES topic (id) ON DELETE CASCADE, -- owning topic
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (topic_id, name)
		);
	`
	if _, err := tx.Exec(ctx, createConsumerGroupSql); err != nil {
		return err
	}

	// consumer group cursors for tracking offset in message_log.
	// UNIQUE keeps group <-> cursor 1:1
	createCursorSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS cursor (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL UNIQUE REFERENCES consumer_group (id) ON DELETE CASCADE,
			claimed BIGINT NOT NULL DEFAULT 0,      -- the read frontier 'inflight' work
			committed BIGINT NOT NULL DEFAULT 0,    -- every message id > committed is in an end state done / dead
			-- the snapshot fence: claims stop at settled_head, not the raw MAX(id),
			-- MAX(id) can sit above uncommitted lower ids -- see FreshClaimMessagesWithCursor
			settled_head BIGINT NOT NULL DEFAULT 0, -- highest id proven to have nothing uncommitted at or below it
			pending_head BIGINT NOT NULL DEFAULT 0, -- candidate head awaiting that proof
			pending_xmax XID8                       -- txid fence read in the same snapshot as pending_head
		);
	`
	if _, err := tx.Exec(ctx, createCursorSql); err != nil {
		return err
	}

	createLeaseSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS lease (
			token UUID NOT NULL DEFAULT gen_random_uuid(),
			consumer_group_id BIGINT NOT NULL,
			low BIGINT NOT NULL,             -- low of claimed range of lease
			high BIGINT NOT NULL,            -- high of claimed range of lease
			until TIMESTAMPTZ NOT NULL,      -- when the lease is considered expired and should be reclaimed
			reclaims INT NOT NULL DEFAULT 0, -- times this range has been reclaimed; past MaxReclaims it's quarantined
			PRIMARY KEY (token, consumer_group_id)
		);
	`
	if _, err := tx.Exec(ctx, createLeaseSql); err != nil {
		return err
	}

	// key_lease: at most one in-flight delivery per compaction key per
	// consumer group.
	createKeyLeaseSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS key_lease (
			consumer_group_id BIGINT NOT NULL, -- PK
			compaction_key TEXT NOT NULL,      -- PK
			lease_token UUID NOT NULL,
			expires_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (consumer_group_id, compaction_key)
		);
	`
	if _, err := tx.Exec(ctx, createKeyLeaseSql); err != nil {
		return err
	}

	// workers: one row per background job that should be running
	createWorkerSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS worker (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES system (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES topic (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES consumer_group (id) ON DELETE CASCADE,
			name TEXT NOT NULL,                      -- 'janitor' | 'cursor_advancer' | 'cron_scheduler' | user-defined
			metadata JSONB NOT NULL DEFAULT '{}',    -- per-worker config, written by the declaration that creates the row
			target_instances INT NOT NULL DEFAULT 1, -- 0 = suspended, -1 = unbounded
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1),
			CHECK (target_instances >= -1)
		);
	`
	if _, err := tx.Exec(ctx, createWorkerSql); err != nil {
		return err
	}

	// one worker of each name per owner: system, topic, group
	for _, indexSql := range []string{
		`-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_topic_name ON worker (name, topic_id) WHERE topic_id IS NOT NULL;`,
		`-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_group_name ON worker (name, consumer_group_id) WHERE consumer_group_id IS NOT NULL;`,
		`-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_system_name ON worker (name, system_id) WHERE system_id IS NOT NULL;`,
	} {
		if _, err := tx.Exec(ctx, indexSql); err != nil {
			return err
		}
	}

	// worker instances: one row per live copy of a worker
	createWorkerInstanceSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS worker_instance (
			id BIGSERIAL PRIMARY KEY,
			worker_id BIGINT NOT NULL REFERENCES worker (id) ON DELETE CASCADE,
			token UUID NOT NULL DEFAULT gen_random_uuid(), -- renew/release match on it, so only the creating instance can touch its row
			expires_at TIMESTAMPTZ NOT NULL,               -- heartbeat-renewed; past it the instance is dead
			attempts INT NOT NULL DEFAULT 0,               -- consecutive run failures. resets on success
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`
	if _, err := tx.Exec(ctx, createWorkerInstanceSql); err != nil {
		return err
	}

	// the two hot lookups: live instances per worker, expired rows
	for _, indexSql := range []string{
		`-- vulkan: system.createSystemTables
CREATE INDEX IF NOT EXISTS worker_instance_worker ON worker_instance (worker_id);`,
		`-- vulkan: system.createSystemTables
CREATE INDEX IF NOT EXISTS worker_instance_expiry ON worker_instance (expires_at);`,
	} {
		if _, err := tx.Exec(ctx, indexSql); err != nil {
			return err
		}
	}

	// bindings: routing rules. A group with no binding matches all events; a
	// group WITH a binding only receives events whose routing_key matches
	// `pattern`.
	createBindingSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS binding (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL REFERENCES consumer_group (id) ON DELETE CASCADE,
			pattern TEXT NOT NULL,                -- POSIX regex translated from the NATS-style pattern
			display TEXT,                         -- original NATS pattern, for humans
			UNIQUE (consumer_group_id, pattern)   -- its index also serves the group lookup
		);
	`
	if _, err := tx.Exec(ctx, createBindingSql); err != nil {
		return err
	}

	// binding_declaration: one row appended per declaration attempt, never
	// updated or deleted.
	// newest installed row per group -> the effective set's declaration
	// newest waiting row per declarer -> its latest still-blocked retry
	// Claims never read this table; the effective set stays in binding rows.
	createBindingDeclarationSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS binding_declaration (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL REFERENCES consumer_group (id) ON DELETE CASCADE,
			status TEXT NOT NULL,                          -- 'installed' | 'waiting'
			patterns TEXT[] NOT NULL,                      -- the full declared set, original NATS-style; empty = whole topic
			declared_by TEXT NOT NULL,                     -- hostname:pid:<random> of the declaring process, display only
			declared_at TIMESTAMPTZ NOT NULL,              -- when this declarer first stated this set; constant across its retries
			attempt_at TIMESTAMPTZ NOT NULL DEFAULT now()  -- when this attempt ran; an installed row's declared_at -> attempt_at is the wait it ended
		);
	`
	if _, err := tx.Exec(ctx, createBindingDeclarationSql); err != nil {
		return err
	}

	// helps listDeclarations so it doesn't have to sequential
	// scan a long wait's appended retry rows
	createBindingDeclarationIndexSql := `-- vulkan: system.createSystemTables
CREATE INDEX IF NOT EXISTS binding_declaration_group ON binding_declaration (consumer_group_id, status, declared_by, id);`
	if _, err := tx.Exec(ctx, createBindingDeclarationIndexSql); err != nil {
		return err
	}

	// O(1) index for compaction's "is this the winner for its key" lookup --
	// upserted synchronously in the same transaction as every keyed publish,
	// never a background job. Shared across topics (not per-topic like
	// message_log) since it scales with DISTINCT compaction_key count, not
	// total message volume.
	createCompactionHeadSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS compaction_head (
			topic_id        BIGINT NOT NULL,           -- PK
			compaction_key  TEXT   NOT NULL,           -- PK
			head_id         BIGINT NOT NULL,           -- the winning message_log id for this key
			compaction_rank BIGINT NOT NULL DEFAULT 0, -- the winner's rank
			PRIMARY KEY (topic_id, compaction_key)
		);
	`
	if _, err := tx.Exec(ctx, createCompactionHeadSql); err != nil {
		return err
	}

	// cron_job: named schedules. Owner FKs are GC metadata only -- all NULL
	// = standalone.
	createCronJobSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS cron_job (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES system (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES topic (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES consumer_group (id) ON DELETE CASCADE,
			name TEXT NOT NULL UNIQUE,                       -- also the routing key every job request is produced with
			schedule TEXT NOT NULL,                          -- cron spec; UTC unless it carries TZ=
			suspended BOOLEAN NOT NULL DEFAULT false,        -- a suspended job keeps its schedule but never produces
			concurrency TEXT NOT NULL DEFAULT 'allow',       -- -> MessageOptions.Concurrency
			timeout_ns BIGINT NOT NULL,                      -- nanoseconds; -> MessageOptions.Timeout
			data JSONB NOT NULL DEFAULT '{}',                -- opaque payload
			metadata JSONB NOT NULL DEFAULT '{}',
			next_scheduled_time TIMESTAMPTZ NOT NULL,
			last_scheduled_time TIMESTAMPTZ,                 -- the scheduled time most recently produced
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1),
			CHECK (concurrency IN ('allow', 'defer')),
			CHECK (timeout_ns > 0)
		);
	`
	if _, err := tx.Exec(ctx, createCronJobSql); err != nil {
		return err
	}

	createCronJobDueIndexSql := `-- vulkan: system.createSystemTables
CREATE INDEX IF NOT EXISTS cron_job_due ON cron_job (next_scheduled_time) WHERE NOT suspended;`
	if _, err := tx.Exec(ctx, createCronJobDueIndexSql); err != nil {
		return err
	}

	// migration_log is the append-only history of migration attempts
	// -- one row per attempt.
	createMigrationLogSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS migration_log (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES system (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES topic (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES consumer_group (id) ON DELETE CASCADE,
			migration_version BIGINT NOT NULL,
			status TEXT NOT NULL,         -- 'success' | 'failure' (extensible)
			error TEXT,                   -- populated when status = 'failure'
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1)
		);
	`
	_, err := tx.Exec(ctx, createMigrationLogSql)
	return err
}
