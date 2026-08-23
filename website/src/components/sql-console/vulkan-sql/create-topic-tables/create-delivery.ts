// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { deliveryTable } from '../table-names';

export const createDeliverySqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			consumer_group_id BIGINT NOT NULL,                -- PK
			message_id BIGINT NOT NULL,                       -- PK
			status TEXT NOT NULL,                             -- 'ready' | 'processing' | 'inflight' | 'deferred' | 'done' | 'dead'
			attempts INT NOT NULL default 0,
			can_run_after TIMESTAMPTZ NOT NULL DEFAULT NOW(), -- backoff between retries
			last_error TEXT,
			lease_token UUID,
			lease_until TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (consumer_group_id, message_id)
		);
	`;

export function createDeliverySql(topicId: number): string {
	return interpolate(createDeliverySqlTemplate, deliveryTable(topicId));
}
