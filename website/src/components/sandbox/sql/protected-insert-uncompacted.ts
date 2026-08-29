// verbatim from pkg/producer/controller/datastore/insert.go protectedInsertSQL -- the
// template is drift-checked byte-exact; the function mirrors the fmt.Sprintf call
import { interpolate } from './interpolate';
import { idempotencyKeyTable, messageLogTable } from './table-names';

export const protectedInsertUncompactedSqlTemplate = `
			-- vulkan: producer.protectedInsert
			WITH claim AS (
				INSERT INTO %s (idempotency_key)
				VALUES ($1)
				ON CONFLICT (idempotency_key) DO NOTHING
				RETURNING idempotency_key
			)
			INSERT INTO %s (payload, routing_key, message_key, options)
			SELECT
				$2,
				NULLIF($3, ''), -- if routing_key is empty string '' insert as NULL
				NULLIF($4, ''), -- if message_key is empty string '' insert as NULL
				$5
			WHERE EXISTS (SELECT 1 FROM claim) -- if claim CTE didn't return anything skip this
			RETURNING id;
		`;

export function protectedInsertUncompactedSql(topicId: number): string {
	return interpolate(
		protectedInsertUncompactedSqlTemplate,
		idempotencyKeyTable(topicId),
		messageLogTable(topicId),
	);
}
