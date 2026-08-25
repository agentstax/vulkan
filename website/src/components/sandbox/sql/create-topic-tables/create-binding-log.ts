// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { bindingLogTable } from '../table-names';

export const createBindingLogSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL REFERENCES consumer_group (id) ON DELETE CASCADE,
			status TEXT NOT NULL,                          -- 'installed' | 'waiting'
			patterns TEXT[] NOT NULL,                      -- the full declared set, original NATS-style; empty = whole topic
			declared_by TEXT NOT NULL,                     -- hostname:pid:<random> of the declaring process, display only
			declared_at TIMESTAMPTZ NOT NULL,              -- when this declarer first stated this set; constant across its retries
			attempt_at TIMESTAMPTZ NOT NULL DEFAULT now()  -- when this attempt ran; an installed row's declared_at -> attempt_at is the wait it ended
		);
	`;

export function createBindingLogSql(topicId: number): string {
	return interpolate(createBindingLogSqlTemplate, bindingLogTable(topicId));
}
