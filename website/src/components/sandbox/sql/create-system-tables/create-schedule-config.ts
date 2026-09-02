// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createScheduleConfigSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS %[1]s.schedule_config (
			id BIGSERIAL PRIMARY KEY,
			system_id BIGINT NOT NULL REFERENCES %[1]s.system_config (id) ON DELETE CASCADE,
			topic_id BIGINT NOT NULL REFERENCES %[1]s.topic_config (id) ON DELETE CASCADE,  -- the target topic every produce lands on
			name TEXT NOT NULL UNIQUE,                       -- also the message key and routing key of every produce
			expression TEXT NOT NULL,                        -- cron expression; UTC unless it carries TZ=
			suspended BOOLEAN NOT NULL DEFAULT false,        -- a suspended schedule keeps its expression but never produces
			concurrency TEXT NOT NULL DEFAULT 'parallel',    -- 'parallel' | 'exclusive' -> MessageOptions.Concurrency
			timeout_ns BIGINT NOT NULL,                      -- nanoseconds; -> MessageOptions.Timeout
			payload JSONB NOT NULL DEFAULT '{}',             -- the message, marshaled once at Register
			schema_version INTEGER NOT NULL,                 -- the payload's Message type version, written on every produce
			metadata JSONB NOT NULL DEFAULT '{}',
			CHECK (timeout_ns > 0)
		);
	`;
