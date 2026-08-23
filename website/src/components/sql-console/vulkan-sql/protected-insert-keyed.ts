// verbatim from pkg/producer/controller/datastore/insert.go protectedInsertSQL -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from './interpolate';
import { idempotencyKeyTable, messageLogTable, compactionHeadTable } from './table-names';

export const protectedInsertKeyedSqlTemplate = `
			-- vulkan: producer.protectedInsert
			WITH claim AS (
				INSERT INTO %s (idempotency_key)
				VALUES ($1)
				ON CONFLICT (idempotency_key) DO NOTHING
				RETURNING idempotency_key
			), inserted AS (
				INSERT INTO %s (payload, routing_key, compaction_key, compaction_rank, options)
				SELECT $2, NULLIF($3, ''), $4, $5, $6  -- if routing_key $3 is empty string '' insert as NULL
				WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
				RETURNING id
			), latest AS (
				INSERT INTO %s AS h (compaction_key, head_id, compaction_rank)
				SELECT $4, id, $5 FROM inserted
				ON CONFLICT (compaction_key) DO UPDATE
				SET head_id = EXCLUDED.head_id, compaction_rank = EXCLUDED.compaction_rank
				-- compare rank first, if rank equal -> head_id is compared
				WHERE (h.compaction_rank, h.head_id) < (EXCLUDED.compaction_rank, EXCLUDED.head_id)
			)
			SELECT id FROM inserted;
		`;

export function protectedInsertKeyedSql(topicId: number): string {
	return interpolate(protectedInsertKeyedSqlTemplate, idempotencyKeyTable(topicId), messageLogTable(topicId), compactionHeadTable(topicId));
}
