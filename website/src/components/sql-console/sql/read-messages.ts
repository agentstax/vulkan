// verbatim from pkg/consumergroup/messageconsumer/controller/datastore/claim.go
// readMessages -- the template is drift-checked byte-exact; the function mirrors
// the fmt.Sprintf call
import { interpolate } from './interpolate';
import { bindingTable, compactionHeadTable, messageLogTable } from './table-names';

export const readMessagesSqlTemplate = `
		-- vulkan: messageconsumer.readMessages
		SELECT
			m.id,
			m.payload,
			m.created_at,
			COALESCE(m.routing_key, '') AS routing_key,
			COALESCE(m.compaction_key, '') AS compaction_key,
			m.compaction_rank,
			m.options
		FROM %s m
		WHERE m.id > $1
			AND m.id <= $2
			AND (
				-- no bindings for consumer_group exists
				NOT EXISTS (
					SELECT 1 FROM %s b
					WHERE b.consumer_group_id = $3
				)
				-- bindings for consumer_group exists and match routing_key pattern
				OR EXISTS (
					SELECT 1 FROM %s b
					WHERE b.consumer_group_id = $3
						AND m.routing_key ~ b.pattern
				)
				-- if bindings exist but our routing_key does not match any of them
				-- we do not return anything
			)
			AND (
				-- unkeyed rows are never compacted
				m.compaction_key IS NULL
				-- keyed rows are eligible only if they're compaction_head's current
				-- pointer for their key -- O(1) lookup, no per-row scan
				OR m.id = (
					SELECT head_id FROM %s
					WHERE compaction_key = m.compaction_key
				)
			)
		-- rows MUST come back in id order or a batch LIMIT could
		-- return an arbitrary subset and the cursor would advance past unread offsets
		ORDER BY m.id;
	`;

export function readMessagesSql(topicId: number): string {
	return interpolate(
		readMessagesSqlTemplate,
		messageLogTable(topicId),
		bindingTable(topicId),
		bindingTable(topicId),
		compactionHeadTable(topicId),
	);
}
