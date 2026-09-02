// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { exceptionQueueTable } from '../table-names';

export const createExceptionQueueSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %[1]s.%[2]s (
			consumer_group_id BIGINT NOT NULL,                -- PK
			message_id BIGINT NOT NULL,                       -- PK
			status TEXT NOT NULL,                             -- 'ready' | 'processing' | 'inflight' | 'deferred' | 'done' | 'dead'
			message_key TEXT,                                 -- the message's key; NULL = keyless
			concurrency TEXT NOT NULL,                        -- 'parallel' | 'exclusive' -- the policy the group resolved for the message when it wrote the row
			attempts INT NOT NULL default 0,                  -- runs so far; the retry budget is attempts - delays
			delays INT NOT NULL DEFAULT 0,                    -- later runs the handler requested, never counted as failures
			can_run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- backoff between retries, or the handler's requested delay
			last_error TEXT,
			lease_token UUID,
			lease_expires_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (consumer_group_id, message_id)
		);
	`;

export function createExceptionQueueSql(topicId: number): string {
	return interpolate(createExceptionQueueSqlTemplate, exceptionQueueTable(topicId));
}
