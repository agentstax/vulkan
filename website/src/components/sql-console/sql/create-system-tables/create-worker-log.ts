// verbatim from pkg/system/controller/datastore/tables.go createSystemTables -- drift-checked byte-exact
export const createWorkerLogSql = `
		-- vulkan: system.createSystemTables
		CREATE TABLE IF NOT EXISTS worker_log (
			id BIGSERIAL PRIMARY KEY,
			worker_id BIGINT NOT NULL REFERENCES worker (id) ON DELETE CASCADE,
			name TEXT NOT NULL,                          -- copied from the worker row so operators scan without a join
			metadata JSONB NOT NULL,
			target_instances INT NOT NULL,
			declared_by TEXT NOT NULL,                   -- hostname:pid:<random> of the declaring process, display only
			declared_at TIMESTAMPTZ NOT NULL DEFAULT now()
		);
	`;
