// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createCronJobConfigSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS cron_job_config (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT REFERENCES system_config (id) ON DELETE CASCADE,
			topic_id BIGINT REFERENCES topic_config (id) ON DELETE CASCADE,
			consumer_group_id BIGINT REFERENCES consumer_group_config (id) ON DELETE CASCADE,
			name TEXT NOT NULL UNIQUE,                       -- also the routing key every job request is produced with
			schedule TEXT NOT NULL,                          -- cron spec; UTC unless it carries TZ=
			suspended BOOLEAN NOT NULL DEFAULT false,        -- a suspended job keeps its schedule but never produces
			concurrency TEXT NOT NULL DEFAULT 'parallel',    -- 'parallel' | 'exclusive' -> MessageOptions.Concurrency
			timeout_ns BIGINT NOT NULL,                      -- nanoseconds; -> MessageOptions.Timeout
			payload JSONB NOT NULL DEFAULT '{}',             -- the job's opaque document, produced with every request
			metadata JSONB NOT NULL DEFAULT '{}',
			CHECK (num_nonnulls(system_id, topic_id, consumer_group_id) = 1),
			CHECK (timeout_ns > 0)
		);
	`;
