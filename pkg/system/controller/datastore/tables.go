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
	createSystemConfigSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS system_config (
			id BIGSERIAL PRIMARY KEY,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
	`
	if _, err := tx.Exec(ctx, createSystemConfigSql); err != nil {
		return err
	}

	createTopicConfigSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS topic_config (
			id BIGSERIAL PRIMARY KEY,                                           -- corresponding id for table interpolation ie message_log_<id>
			system_id BIGINT NOT NULL REFERENCES system_config (id) ON DELETE CASCADE, -- owning system
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
	if _, err := tx.Exec(ctx, createTopicConfigSql); err != nil {
		return err
	}

	// topic_config_log: one full-snapshot row appended in the same transaction as
	// every topic create, config replace, and rename -- never updated or
	// deleted. The topic_config row is the truth; this trail is for operators.
	createTopicConfigLogSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS topic_config_log (
			id BIGSERIAL PRIMARY KEY,
			topic_id BIGINT NOT NULL REFERENCES topic_config (id) ON DELETE CASCADE,
			name TEXT NOT NULL,                          -- the topic's name as of this declaration
			partition_size BIGINT NOT NULL,
			retention_ttl_ns BIGINT NOT NULL,
			allow_drop_past_committed BOOLEAN NOT NULL,
			idempotency_key_ttl_ns BIGINT NOT NULL,
			delivery_log_mode TEXT NOT NULL,
			declared_by TEXT NOT NULL,                   -- hostname:pid:<random> of the declaring process, display only
			declared_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`
	if _, err := tx.Exec(ctx, createTopicConfigLogSql); err != nil {
		return err
	}

	// the one lookup shape: a topic's rows in change order
	createTopicConfigLogIndexSql := `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS topic_config_log_topic ON topic_config_log (topic_id, id);
	`
	if _, err := tx.Exec(ctx, createTopicConfigLogIndexSql); err != nil {
		return err
	}

	// consumer_group_config table provides:
	// - lifcycle management for child ownershipt model (cursor, binding, maintainence)
	createConsumerGroupConfigSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS consumer_group_config (
			id BIGSERIAL PRIMARY KEY,                                         -- what children reference
			topic_id BIGINT NOT NULL REFERENCES topic_config (id) ON DELETE CASCADE, -- owning topic
			name TEXT NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE (topic_id, name)
		);
	`
	if _, err := tx.Exec(ctx, createConsumerGroupConfigSql); err != nil {
		return err
	}

	// workers: one row per background job that should be running
	createWorkerConfigSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS worker_config (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES system_config (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES topic_config (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES consumer_group_config (id) ON DELETE CASCADE,
			name TEXT NOT NULL,                      -- 'janitor' | 'cursor_advancer' | 'cron_scheduler' | user-defined
			metadata JSONB NOT NULL DEFAULT '{}',    -- per-worker config, written by the declaration that creates the row
			target_instances INT NOT NULL DEFAULT 1, -- 0 = suspended, -1 = unbounded
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1),
			CHECK (target_instances >= -1)
		);
	`
	if _, err := tx.Exec(ctx, createWorkerConfigSql); err != nil {
		return err
	}

	// one worker of each name per owner: system, topic, group
	for _, indexSql := range []string{
		`-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_config_topic_name ON worker_config (name, topic_id) WHERE topic_id IS NOT NULL;`,
		`-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_config_group_name ON worker_config (name, consumer_group_id) WHERE consumer_group_id IS NOT NULL;`,
		`-- vulkan: system.createSystemTables
CREATE UNIQUE INDEX IF NOT EXISTS worker_config_system_name ON worker_config (name, system_id) WHERE system_id IS NOT NULL;`,
	} {
		if _, err := tx.Exec(ctx, indexSql); err != nil {
			return err
		}
	}

	// worker_config_log: one full-snapshot row appended in the same transaction as
	// every worker create and metadata replace.
	// The worker_config row is the truth; this trail is for operators.
	createWorkerConfigLogSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS worker_config_log (
			id BIGSERIAL PRIMARY KEY,
			worker_id BIGINT NOT NULL REFERENCES worker_config (id) ON DELETE CASCADE,
			name TEXT NOT NULL,                          -- copied from the worker row so operators scan without a join
			metadata JSONB NOT NULL,
			target_instances INT NOT NULL,
			declared_by TEXT NOT NULL,                   -- hostname:pid:<random> of the declaring process, display only
			declared_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`
	if _, err := tx.Exec(ctx, createWorkerConfigLogSql); err != nil {
		return err
	}

	// the one lookup shape: a worker's rows in change order
	createWorkerConfigLogIndexSql := `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS worker_config_log_worker ON worker_config_log (worker_id, id);
	`
	if _, err := tx.Exec(ctx, createWorkerConfigLogIndexSql); err != nil {
		return err
	}

	// worker instances: one row per live copy of a worker
	createWorkerInstanceSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS worker_instance (
			id BIGSERIAL PRIMARY KEY,
			worker_id BIGINT NOT NULL REFERENCES worker_config (id) ON DELETE CASCADE,
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
		`
			-- vulkan: system.createSystemTables
			CREATE INDEX IF NOT EXISTS worker_instance_worker ON worker_instance (worker_id);
		`,
		`
			-- vulkan: system.createSystemTables
			CREATE INDEX IF NOT EXISTS worker_instance_expiry ON worker_instance (expires_at);
		`,
	} {
		if _, err := tx.Exec(ctx, indexSql); err != nil {
			return err
		}
	}

	// cron_job_config: named schedules. Owner FKs are GC metadata only -- all
	// NULL = standalone.
	createCronJobConfigSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS cron_job_config (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES system_config (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES topic_config (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES consumer_group_config (id) ON DELETE CASCADE,
			name TEXT NOT NULL UNIQUE,                       -- also the routing key every job request is produced with
			schedule TEXT NOT NULL,                          -- cron spec; UTC unless it carries TZ=
			suspended BOOLEAN NOT NULL DEFAULT false,        -- a suspended job keeps its schedule but never produces
			concurrency TEXT NOT NULL DEFAULT 'allow',       -- -> MessageOptions.Concurrency
			timeout_ns BIGINT NOT NULL,                      -- nanoseconds; -> MessageOptions.Timeout
			payload JSONB NOT NULL DEFAULT '{}',             -- the job's opaque document, produced with every request
			metadata JSONB NOT NULL DEFAULT '{}',
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1),
			CHECK (concurrency IN ('allow', 'defer')),
			CHECK (timeout_ns > 0)
		);
	`
	if _, err := tx.Exec(ctx, createCronJobConfigSql); err != nil {
		return err
	}

	// cron_job_cursor: the scheduler's position in each job's schedule --
	// the runtime sibling of the near-static config row, 1:1 by cron_job_id.
	createCronJobCursorSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS cron_job_cursor (
			cron_job_id BIGINT NOT NULL PRIMARY KEY REFERENCES cron_job_config (id) ON DELETE CASCADE,
			next_scheduled_at TIMESTAMPTZ NOT NULL,
			last_scheduled_at TIMESTAMPTZ               -- the scheduled time most recently produced
		);
	`
	if _, err := tx.Exec(ctx, createCronJobCursorSql); err != nil {
		return err
	}

	// the due scan; suspended lives on the config row, so the filter is the
	// scan's join, not this index
	createCronJobCursorDueIndexSql := `
		-- vulkan: system.createSystemTables
		CREATE INDEX IF NOT EXISTS cron_job_cursor_due ON cron_job_cursor (next_scheduled_at);
	`
	if _, err := tx.Exec(ctx, createCronJobCursorDueIndexSql); err != nil {
		return err
	}

	// migration_log is the append-only history of migration attempts
	// -- one row per attempt.
	createMigrationLogSql := `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS migration_log (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES system_config (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES topic_config (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES consumer_group_config (id) ON DELETE CASCADE,
			migration_version BIGINT NOT NULL,
			min_compatible_version BIGINT NOT NULL DEFAULT 0, -- the step's MinCompatibleVersion; 0 on baseline and down rows
			status TEXT NOT NULL,                             -- 'success' | 'failure' (extensible)
			error TEXT,                                       -- populated when status = 'failure'
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1)
		);
	`
	_, err := tx.Exec(ctx, createMigrationLogSql)
	return err
}
