// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { bindingConfigLogTable } from '../table-names';

export const createBindingConfigLogSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL REFERENCES consumer_group_config (id) ON DELETE CASCADE,
			status TEXT NOT NULL,                            -- 'installed' | 'waiting'
			patterns TEXT[] NOT NULL,                        -- the full declared set, original NATS-style; empty = whole topic
			declared_by TEXT NOT NULL,                       -- hostname:pid:<random> of the declaring process, display only
			declared_at TIMESTAMPTZ NOT NULL,                -- when this declarer first stated this set; constant across its retries
			attempted_at TIMESTAMPTZ NOT NULL DEFAULT now()  -- when this attempt ran; an installed row's declared_at -> attempted_at is the wait it ended
		);
	`;

export function createBindingConfigLogSql(topicId: number): string {
	return interpolate(createBindingConfigLogSqlTemplate, bindingConfigLogTable(topicId));
}
