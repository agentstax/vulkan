// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { bindingTable } from '../table-names';

export const createBindingSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL REFERENCES consumer_group (id) ON DELETE CASCADE,
			pattern TEXT NOT NULL,                -- POSIX regex translated from the NATS-style pattern
			display TEXT,                         -- original NATS pattern, for humans
			UNIQUE (consumer_group_id, pattern)   -- its index also serves the group lookup
		);
	`;

export function createBindingSql(topicId: number): string {
	return interpolate(createBindingSqlTemplate, bindingTable(topicId));
}
