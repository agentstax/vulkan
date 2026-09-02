// verbatim from pkg/topic/controller/datastore/tables.go createTopicTables -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from '../interpolate';
import { consumerGroupCursorTable } from '../table-names';

export const createConsumerGroupCursorSqlTemplate = `
		-- vulkan: topic.createTopicTables
		CREATE TABLE IF NOT EXISTS %[1]s.%[2]s (
			id BIGSERIAL PRIMARY KEY,
			consumer_group_id BIGINT NOT NULL UNIQUE REFERENCES %[1]s.consumer_group_config (id) ON DELETE CASCADE,
			claimed BIGINT NOT NULL DEFAULT 0,      -- the read frontier 'inflight' work
			committed BIGINT NOT NULL DEFAULT 0,    -- every message id > committed is in an end state done / dead
			-- the snapshot fence: claims stop at settled_head, not the raw MAX(id),
			-- MAX(id) can sit above uncommitted lower ids -- see FreshClaimMessagesWithCursor
			settled_head BIGINT NOT NULL DEFAULT 0, -- highest id proven to have nothing uncommitted at or below it
			pending_head BIGINT NOT NULL DEFAULT 0, -- candidate head awaiting that proof
			pending_xmax XID8                       -- txid fence read in the same snapshot as pending_head
		);
	`;

export function createConsumerGroupCursorSql(topicId: number): string {
	return interpolate(createConsumerGroupCursorSqlTemplate, consumerGroupCursorTable(topicId));
}
