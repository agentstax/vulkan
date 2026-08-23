// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createCronJobSql = `
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
	`;
