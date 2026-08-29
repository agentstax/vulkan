// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerInstanceSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS worker_instance (
			id BIGSERIAL PRIMARY KEY,
			worker_id BIGINT NOT NULL REFERENCES worker_config (id) ON DELETE CASCADE,
			token UUID NOT NULL DEFAULT gen_random_uuid(), -- renew/release match on it, so only the creating instance can touch its row
			expires_at TIMESTAMPTZ NOT NULL,               -- heartbeat-renewed; past it the instance is dead
			attempts INT NOT NULL DEFAULT 0,               -- consecutive run failures. resets on success
			created_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`;
